package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
)

type PageData struct {
	Title    string
	Active   string // current tab (settings or monitoring)
	Path     string // current address
	Section  string // second part of the address, e.g. "dashboard" (highlights the sidebar item)
	Initials string // letters shown in the top-right avatar
	Data     any    // page-specific data
}

// ---------- User dashboard data ----------

type Job struct {
	ID        string
	Name      string
	Status    string
	Class     string // processing, completed or failed (badge colour)
	Submitted string
	Action    string
	ActionURL string
	Percent   int
}

type DashboardData struct {
	Total, Completed, Processing, Failed int
	Jobs                                 []Job
}

// ---------- Admin dashboard data ----------

type AdminStat struct {
	Label      string
	Value      string
	Trend      string
	TrendClass string // good (green) or bad (red)
	Class      string // card colour: "", completed, processing or failed
}

type QueueShare struct {
	Name    string
	Percent int
	Class   string // c1 to c6 (legend colour)
}

type RecentJob struct {
	ID     string
	Name   string
	Status string
}

type AdminDashboardData struct {
	Stats     []AdminStat
	TotalJobs string
	Queues    []QueueShare
	Jobs      []RecentJob
}

// ---------- Admin jobs list data ----------

type AdminJob struct {
	ID            string
	Name          string
	Queue         string
	Priority      string
	PriorityClass string // high, medium or low
	Status        string
	StatusClass   string // processing, completed, failed or queued
	Worker        string
	Duration      string
	Submitted     string
}

type AdminJobsData struct {
	Total    string
	Jobs     []AdminJob
	Query    string // text typed in the filter box
	Status   string // selected status ("" means all)
	Queue    string // selected queue ("" means all)
	Filtered bool   // true when any filter is active
}

// ---------- Admin workers data ----------

type SimpleStat struct {
	Label string
	Value string
}

type Worker struct {
	Name        string
	Host        string
	CPU         int // percent, drives the CPU bar
	Memory      int // percent, drives the memory bar
	Status      string
	StatusClass string // online, busy or offline
	Job         string
	LastSeen    string
}

type AdminWorkersData struct {
	Stats   []SimpleStat
	Workers []Worker
}

// ---------- Admin queues data ----------

type Queue struct {
	Name          string
	Priority      string
	PriorityClass string // high, normal or low
	Pending       int
	Workers       int
	Scheduling    string
}

type AdminQueuesData struct {
	Queues []Queue
}

// ---------- Admin monitoring: system overview data ----------

type Metric struct {
	Label  string
	Value  string
	Color  string // line colour
	Points string // ready-made SVG points for the line
}

type AdminMonitoringData struct {
	Metrics []Metric
}

// ---------- Admin monitoring: resource usage data ----------

type ResourceCard struct {
	Label   string
	Value   string
	Percent int    // drives the bar length
	Caption string // small text under the bar
}

type ResourceWorker struct {
	Name        string
	Status      string
	StatusClass string // online, busy or offline
	CPU         int
	Memory      int
	Disk        int
	Network     string
}

type AdminResourcesData struct {
	Cards   []ResourceCard
	Workers []ResourceWorker
}

// ---------- Admin monitoring: job performance data ----------

type PerfCard struct {
	Label      string
	Value      string
	Trend      string
	TrendClass string // good (green) or bad (red)
}

type HourBar struct {
	Hour    string
	Value   int
	Height  int  // bar height in pixels
	Current bool // the latest hour is drawn in a lighter colour
}

type QueueDuration struct {
	Name     string
	Duration string
	Percent  int // drives the bar length
}

type AdminPerformanceData struct {
	Cards  []PerfCard
	Hours  []HourBar
	Queues []QueueDuration
}

// ---------- Admin logs data ----------

type LogEntry struct {
	Time       string
	Level      string // INFO, WARN or ERROR
	LevelClass string // info, warn or error (pill colour)
	Message    string
}

type AdminLogsData struct {
	Logs     []LogEntry
	Level    string // selected level ("" means all)
	Query    string // text typed in the search box
	Filtered bool   // true when any filter is active
}

// sparkline turns a list of readings into SVG points (400 wide, 100 high).
// lo and hi are the lowest and highest value the chart should show.
func sparkline(values []int, lo, hi int) string {
	var b strings.Builder
	step := 400.0 / float64(len(values)-1)
	for i, v := range values {
		x := float64(i) * step
		y := 92 - float64(v-lo)/float64(hi-lo)*84
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	return b.String()
}

var initials = map[string]string{"admin": "AD", "user": "DU", "worker": "WK"}

// render builds base.html + the role's sidebar + one page template
func render(w http.ResponseWriter, r *http.Request, role, file string, p PageData) {
	tmpl, err := template.ParseFiles(
		"templates/base.html",
		"templates/sidebars/"+role+".html",
		"templates/partials/logo.html",
		"templates/partials/settings-tabs.html",
		"templates/partials/monitoring-tabs.html",
		"templates/"+file,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.Path = r.URL.Path
	if parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/"); len(parts) >= 2 {
		p.Section = parts[1]
	}
	p.Initials = initials[role]
	if err := tmpl.ExecuteTemplate(w, "base.html", p); err != nil {
		log.Println(err)
	}
}

func page(title, role, file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, role, file, PageData{Title: title})
	}
}

func settingsPage(title, tab, file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "admin", file, PageData{Title: title, Active: tab})
	}
}

// loginPage renders a standalone page (no base layout, no sidebar)
func loginPage(file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("templates/" + file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tmpl.Execute(w, nil); err != nil {
			log.Println(err)
		}
	}
}

// userDashboard shows sample data for now; the database will replace it later
func userDashboard(w http.ResponseWriter, r *http.Request) {
	data := DashboardData{
		Total: 28, Completed: 19, Processing: 6, Failed: 3,
		Jobs: []Job{
			{"#J-10432", "Generate thumbnails", "Processing", "processing", "Oct 16, 14:32", "View", "/user/job-details", 80},
			{"#J-10431", "Analyze dataset", "Completed", "completed", "Oct 16, 13:18", "Results", "/user/results", 100},
			{"#J-10430", "Encode video", "Completed", "completed", "Oct 16, 10:45", "Results", "/user/results", 100},
			{"#J-10429", "Send email", "Failed", "failed", "Oct 16, 09:22", "View", "/user/job-details", 0},
			{"#J-10428", "Process image", "Completed", "completed", "Oct 16, 08:17", "Results", "/user/results", 100},
		},
	}
	render(w, r, "user", "user/dashboard.html", PageData{Title: "Dashboard", Data: data})
}

// adminDashboard shows sample data for now; the database will replace it later
func adminDashboard(w http.ResponseWriter, r *http.Request) {
	data := AdminDashboardData{
		Stats: []AdminStat{
			{Label: "Total Jobs", Value: "1,248", Trend: "▲ 12% this week", TrendClass: "good", Class: ""},
			{Label: "Completed", Value: "1,162", Trend: "▲ 15% this week", TrendClass: "good", Class: "completed"},
			{Label: "Processing", Value: "52", Trend: "▼ 8% this week", TrendClass: "bad", Class: "processing"},
			{Label: "Failed", Value: "34", Trend: "▲ 26% this week", TrendClass: "bad", Class: "failed"},
		},
		TotalJobs: "1,248",
		Queues: []QueueShare{
			{"image-processing", 32, "c1"},
			{"data-analysis", 22, "c2"},
			{"video-encoding", 18, "c3"},
			{"email-sending", 14, "c4"},
			{"default", 8, "c5"},
			{"other", 6, "c6"},
		},
		Jobs: []RecentJob{
			{"job-10427", "Report generation", "Completed"},
			{"job-10430", "Encode video", "Running"},
			{"job-10426", "Image processing", "Queued"},
		},
	}
	render(w, r, "admin", "admin/dashboard.html", PageData{Title: "Dashboard", Data: data})
}

// sample jobs for now; the database will replace this later
var allJobs = []AdminJob{
	{"#J-10432", "Generate thumbnails", "image-processing", "High", "high", "Processing", "processing", "worker-3", "3m 24s", "12 min ago"},
	{"#J-10431", "Analyze dataset", "data-analysis", "Medium", "medium", "Completed", "completed", "worker-7", "2m 11s", "10 min ago"},
	{"#J-10430", "Encode video", "video-encoding", "Low", "low", "Processing", "processing", "worker-5", "5m 02s", "15 min ago"},
	{"#J-10429", "Send welcome email", "email-sending", "Medium", "medium", "Completed", "completed", "worker-2", "1m 12s", "12 min ago"},
	{"#J-10428", "Process image", "image-processing", "High", "high", "Failed", "failed", "worker-6", "45s", "13 min ago"},
	{"#J-10427", "Generate report", "default", "Low", "low", "Completed", "completed", "worker-4", "3m 37s", "20 min ago"},
	{"#J-10426", "Resize batch", "image-processing", "Medium", "medium", "Queued", "queued", "—", "—", "10 min ago"},
}

// adminJobs shows the jobs list and applies the filters from the address,
// for example /admin/jobs?q=video&status=Processing&queue=video-encoding
func adminJobs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	queue := r.URL.Query().Get("queue")

	var shown []AdminJob
	for _, j := range allJobs {
		if q != "" {
			text := strings.ToLower(j.ID + " " + j.Name + " " + j.Queue + " " + j.Worker)
			if !strings.Contains(text, strings.ToLower(q)) {
				continue
			}
		}
		if status != "" && j.Status != status {
			continue
		}
		if queue != "" && j.Queue != queue {
			continue
		}
		shown = append(shown, j)
	}

	data := AdminJobsData{
		Total:    "1,248",
		Jobs:     shown,
		Query:    q,
		Status:   status,
		Queue:    queue,
		Filtered: q != "" || status != "" || queue != "",
	}
	render(w, r, "admin", "admin/jobs.html", PageData{Title: "Jobs List", Data: data})
}

// adminWorkers shows sample data for now; the database will replace it later
func adminWorkers(w http.ResponseWriter, r *http.Request) {
	data := AdminWorkersData{
		Stats: []SimpleStat{
			{"Online", "6"},
			{"Busy", "3"},
			{"Offline", "1"},
			{"Avg. CPU", "34%"},
		},
		Workers: []Worker{
			{"worker-1", "192.168.1.10", 38, 45, "Online", "online", "—", "2m ago"},
			{"worker-2", "192.168.1.11", 26, 22, "Online", "online", "—", "3m ago"},
			{"worker-3", "192.168.1.12", 72, 60, "Busy", "busy", "#J-10432", "1m ago"},
			{"worker-4", "192.168.1.13", 33, 40, "Online", "online", "—", "4m ago"},
			{"worker-5", "192.168.1.14", 82, 68, "Busy", "busy", "#J-10430", "1m ago"},
			{"worker-6", "192.168.1.15", 1, 1, "Offline", "offline", "—", "42m ago"},
			{"worker-7", "192.168.1.16", 29, 35, "Online", "online", "—", "5m ago"},
		},
	}
	render(w, r, "admin", "admin/workers.html", PageData{Title: "Workers", Data: data})
}

// adminQueues shows sample data for now; the database will replace it later
func adminQueues(w http.ResponseWriter, r *http.Request) {
	data := AdminQueuesData{
		Queues: []Queue{
			{"image-processing", "High", "high", 12, 3, "Round Robin"},
			{"data-analysis", "High", "high", 8, 2, "Round Robin"},
			{"video-encoding", "Normal", "normal", 6, 2, "Round Robin"},
			{"email-sending", "Normal", "normal", 4, 1, "Round Robin"},
			{"default", "Low", "low", 2, 1, "Round Robin"},
		},
	}
	render(w, r, "admin", "admin/queues.html", PageData{Title: "Queues", Data: data})
}

// adminMonitoring shows sample readings for now; real metrics will replace them later
func adminMonitoring(w http.ResponseWriter, r *http.Request) {
	data := AdminMonitoringData{
		Metrics: []Metric{
			{"CPU Usage", "42%", "#0d9488", sparkline([]int{20, 26, 16, 34, 30, 42, 32, 46, 38, 52, 46}, 0, 60)},
			{"Memory Usage", "68%", "#5fe0a0", sparkline([]int{30, 34, 36, 40, 38, 45, 43, 48, 46, 52, 54}, 0, 70)},
			{"Network I/O", "12 MB/s", "#134e4a", sparkline([]int{10, 40, 25, 50, 22, 34, 18, 46, 30, 38, 26}, 0, 55)},
			{"Disk Usage", "55%", "#0d9488", sparkline([]int{50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60}, 0, 100)},
		},
	}
	render(w, r, "admin", "admin/monitoring/overview.html", PageData{Title: "Monitoring", Active: "overview", Data: data})
}

// adminResources shows sample data for now; the database will replace it later
func adminResources(w http.ResponseWriter, r *http.Request) {
	data := AdminResourcesData{
		Cards: []ResourceCard{
			{"CPU Usage", "42%", 42, "Cluster average"},
			{"Memory Usage", "68%", 68, "Cluster average"},
			{"Disk Usage", "55%", 55, "Cluster average"},
			{"Network I/O", "12 MB/s", 12, "of 100 MB/s capacity"},
		},
		Workers: []ResourceWorker{
			{"worker-1", "Online", "online", 38, 45, 30, "1.5 MB/s"},
			{"worker-2", "Online", "online", 26, 22, 28, "1.0 MB/s"},
			{"worker-3", "Busy", "busy", 72, 61, 62, "3.2 MB/s"},
			{"worker-4", "Online", "online", 33, 40, 35, "1.3 MB/s"},
			{"worker-5", "Busy", "busy", 81, 68, 58, "3.8 MB/s"},
			{"worker-6", "Offline", "offline", 0, 0, 0, "—"},
			{"worker-7", "Online", "online", 29, 35, 33, "1.2 MB/s"},
		},
	}
	render(w, r, "admin", "admin/monitoring/resources.html", PageData{Title: "Monitoring", Active: "resources", Data: data})
}

// adminPerformance shows sample data for now; the database will replace it later
func adminPerformance(w http.ResponseWriter, r *http.Request) {
	hours := []string{"08", "09", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19"}
	counts := []int{38, 45, 52, 41, 36, 48, 55, 47, 40, 44, 51, 42}

	bars := make([]HourBar, len(hours))
	for i := range hours {
		bars[i] = HourBar{
			Hour:    hours[i],
			Value:   counts[i],
			Height:  counts[i] * 150 / 55, // the tallest bar (55) is 150px high
			Current: i == len(hours)-1,
		}
	}

	data := AdminPerformanceData{
		Cards: []PerfCard{
			{"Avg. job duration", "3m 12s", "▼ 6% vs last week", "good"},
			{"Success rate", "93.1%", "▲ 1.2% vs last week", "good"},
			{"Avg. queue wait", "18s", "▼ 4s vs last week", "good"},
			{"Throughput", "42 jobs/hour", "▲ 8% vs last week", "good"},
		},
		Hours: bars,
		Queues: []QueueDuration{
			{"image-processing", "3m 24s", 41},
			{"data-analysis", "2m 11s", 26},
			{"video-encoding", "5m 02s", 60},
			{"email-sending", "1m 12s", 14},
			{"default", "3m 37s", 43},
		},
	}
	render(w, r, "admin", "admin/monitoring/performance.html", PageData{Title: "Monitoring", Active: "performance", Data: data})
}

// sample log lines for now; real logs will replace this later
var allLogs = []LogEntry{
	{"2026-10-16 14:32", "INFO", "info", "Job #J-10431 completed successfully"},
	{"2026-10-16 14:28", "WARN", "warn", "Worker-5 high memory usage (71%)"},
	{"2026-10-16 14:25", "ERROR", "error", "Job #J-10428 failed: out of memory"},
	{"2026-10-16 14:20", "INFO", "info", "Worker-3 connected"},
	{"2026-10-16 14:15", "INFO", "info", "New job submitted: #J-10433"},
	{"2026-10-16 14:10", "INFO", "info", "Queue \"video-encoding\" scaled"},
}

// adminLogs shows the log list and applies the filters from the address,
// for example /admin/logs?level=ERROR&q=memory
func adminLogs(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var shown []LogEntry
	for _, l := range allLogs {
		if level != "" && l.Level != level {
			continue
		}
		if q != "" {
			text := strings.ToLower(l.Time + " " + l.Message)
			if !strings.Contains(text, strings.ToLower(q)) {
				continue
			}
		}
		shown = append(shown, l)
	}

	data := AdminLogsData{
		Logs:     shown,
		Level:    level,
		Query:    q,
		Filtered: level != "" || q != "",
	}
	render(w, r, "admin", "admin/logs.html", PageData{Title: "Logs", Data: data})
}

func main() {
	// Public pages (no sidebar)
	http.HandleFunc("/{$}", page("Home", "none", "home.html"))
	http.HandleFunc("/login", page("Login", "none", "login.html"))

	// Admin login (standalone page)
	http.HandleFunc("GET /admin/login", loginPage("admin/login.html"))
	// Temporary: real authentication comes later. For now, Sign In just opens the dashboard.
	http.Handle("POST /admin/login", http.RedirectHandler("/admin/dashboard", http.StatusSeeOther))

	// Admin
	http.Handle("/admin/{$}", http.RedirectHandler("/admin/dashboard", http.StatusFound))
	http.HandleFunc("/admin/dashboard", adminDashboard)
	http.HandleFunc("/admin/jobs", adminJobs)
	http.HandleFunc("/admin/workers", adminWorkers)
	http.HandleFunc("/admin/queues", adminQueues)
	http.HandleFunc("/admin/logs", adminLogs)

	// Admin monitoring (with tabs)
	http.Handle("/admin/monitoring", http.RedirectHandler("/admin/monitoring/overview", http.StatusFound))
	http.HandleFunc("/admin/monitoring/overview", adminMonitoring)
	http.HandleFunc("/admin/monitoring/resources", adminResources)
	http.HandleFunc("/admin/monitoring/performance", adminPerformance)

	// Admin settings (with tabs)
	http.Handle("/admin/settings", http.RedirectHandler("/admin/settings/general", http.StatusFound))
	http.HandleFunc("/admin/settings/general", settingsPage("System Settings", "general", "admin/settings/general.html"))
	http.HandleFunc("/admin/settings/workers", settingsPage("System Settings", "workers", "admin/settings/workers.html"))
	http.HandleFunc("/admin/settings/queues", settingsPage("System Settings", "queues", "admin/settings/queues.html"))
	http.HandleFunc("/admin/settings/security", settingsPage("System Settings", "security", "admin/settings/security.html"))
	http.HandleFunc("/admin/settings/notifications", settingsPage("System Settings", "notifications", "admin/settings/notifications.html"))

	// User
	http.Handle("/user/{$}", http.RedirectHandler("/user/dashboard", http.StatusFound))
	http.HandleFunc("/user/dashboard", userDashboard)
	http.HandleFunc("/user/submit-job", page("Submit Job", "user", "user/submit-job.html"))
	http.HandleFunc("/user/job-details", page("Job Details", "user", "user/job-details.html"))
	http.HandleFunc("/user/results", page("Results", "user", "user/results.html"))
	http.HandleFunc("/user/my-jobs", page("My Jobs", "user", "user/my-jobs.html"))

	// Worker
	http.Handle("/worker/{$}", http.RedirectHandler("/worker/dashboard", http.StatusFound))
	http.HandleFunc("/worker/dashboard", page("Worker Dashboard", "worker", "worker/dashboard.html"))
	http.HandleFunc("/worker/tasks", page("Assigned Tasks", "worker", "worker/tasks.html"))

	// CSS and images
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Println("Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
