package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/lsongdev/smartctl-go/smartctl"
	"github.com/lsongdev/smartctl-go/templates"
	"gopkg.in/yaml.v2"
)

type H map[string]interface{}

type Report struct {
	ID          int       `db:"id"`
	Name        string    `db:"name"`
	Device      string    `db:"device"`
	Temperature int64     `db:"temperature"`
	Status      int       `db:"status"`
	FileName    string    `db:"path"`
	CreatedAt   time.Time `db:"created_at"`
}

type Config struct {
	Listen string   `yaml:"listen"`
	Disks  []string `yaml:"disks"`
}

func LoadConfig() (config *Config, err error) {
	f, err := os.Open("config.yaml")
	if err != nil {
		return
	}
	defer f.Close()
	err = yaml.NewDecoder(f).Decode(&config)
	return
}

type Server struct {
	db     *sql.DB
	config *Config
}

func NewServer() (server *Server, err error) {
	config, err := LoadConfig()
	if err != nil {
		return
	}
	db, err := sql.Open("sqlite", "smart.db")
	if err != nil {
		return
	}
	server = &Server{db, config}
	server.Init()
	return
}

func (s *Server) Init() (err error) {
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS reports (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				device TEXT NOT NULL,
				temperature INTEGER,
				status BOOLEAN,
				path TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
  `)
	return
}

func (s *Server) Insert(report *Report) (err error) {
	_, err = s.db.Exec(`INSERT INTO reports (name, device, temperature, status, path) VALUES (?, ?, ?, ?, ?)`,
		report.Name,
		report.Device,
		report.Temperature,
		report.Status,
		report.FileName,
	)
	return
}

func (s *Server) GetReport(device string) (report *Report, err error) {
	row := s.db.QueryRow("SELECT * FROM reports WHERE device = ? ORDER BY created_at DESC", device)
	report = &Report{}
	err = row.Scan(
		&report.ID,
		&report.Name,
		&report.Device,
		&report.Temperature,
		&report.Status,
		&report.FileName,
		&report.CreatedAt,
	)
	return
}

func (s *Server) RunCheck(device string) (err error) {
	info, err := smartctl.Check(device)
	if err != nil {
		return
	}

	report := &Report{
		Name:        info.ModelName,
		Device:      info.Device.Name,
		Temperature: info.Temperature.Current,
	}
	if info.SmartStatus.Passed {
		report.Status = 1
	} else {
		report.Status = 2
	}
	timestamp := time.Now().Format("20060102_150405")
	report.FileName = filepath.Join("reports", fmt.Sprintf("smart_%s.json", timestamp))
	os.MkdirAll(filepath.Dir(report.FileName), 0755)
	f, err := os.Create(report.FileName)
	if err != nil {
		return
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(info); err != nil {
		return
	}
	err = s.Insert(report)
	return
}

func (s *Server) CheckAll() {
	for _, dev := range s.config.Disks {
		err := s.RunCheck(dev)
		if err != nil {
			log.Println(err)
		}
	}
}

func (s *Server) StartScheduler() {
	s.CheckAll()
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			s.CheckAll()
		}
	}()
}

func (s *Server) Render(w http.ResponseWriter, name string, data H) {
	if data == nil {
		data = H{}
	}
	// tmpl, err := template.ParseFiles("templates/layout.html", "templates/"+name+".html")
	tmpl, err := template.New("").ParseFS(templates.Files, "layout.html", name+".html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) Error(w http.ResponseWriter, err error) {
	s.Render(w, "error", H{
		"error": err,
	})
}

func (s *Server) IndexView(w http.ResponseWriter, r *http.Request) {
	reports := make([]Report, 0)
	for _, dev := range s.config.Disks {
		report, err := s.GetReport(dev)
		if err != nil {
			continue
		}
		reports = append(reports, *report)
	}
	s.Render(w, "index", H{
		"reports": reports,
	})
}

func (s *Server) ReportView(w http.ResponseWriter, r *http.Request) {
	dev := r.URL.Query().Get("dev")
	report, err := s.GetReport(dev)
	if err != nil {
		s.Error(w, err)
		return
	}
	info, err := smartctl.Open(report.FileName)
	if err != nil {
		s.Error(w, err)
		return
	}
	s.Render(w, "report", H{
		"info": info,
	})
}
