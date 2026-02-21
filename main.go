package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	levelStepXP = 200
)

//go:embed web/index.html
var indexHTML []byte

type TierDef struct {
	Name   string `json:"name"`
	Target int    `json:"target"`
}

type AchievementDef struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Metric      string    `json:"metric"`
	Tiers       []TierDef `json:"tiers"`
}

type AppState struct {
	XP             int             `json:"xp"`
	StudyHours     int             `json:"study_hours"`
	SkillModules   int             `json:"skill_modules"`
	Projects       int             `json:"projects"`
	BugFixes       int             `json:"bug_fixes"`
	Reflections    int             `json:"reflections"`
	GitCommits     int             `json:"git_commits"`
	CheckinDates   map[string]bool `json:"checkin_dates"`
	WeeklyCheckins map[string]int  `json:"weekly_checkins"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ActionRequest struct {
	Kind   string `json:"kind"`
	Amount int    `json:"amount"`
}

type AchievementProgress struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Value       int     `json:"value"`
	CurrentTier int     `json:"current_tier"`
	TierName    string  `json:"tier_name"`
	NextTarget  int     `json:"next_target"`
	Progress    float64 `json:"progress"`
}

type DashboardResponse struct {
	XP              int                   `json:"xp"`
	Level           int                   `json:"level"`
	CurrentLevelXP  int                   `json:"current_level_xp"`
	NextLevelXP     int                   `json:"next_level_xp"`
	Streak          int                   `json:"streak"`
	StudyHours      int                   `json:"study_hours"`
	SkillModules    int                   `json:"skill_modules"`
	Projects        int                   `json:"projects"`
	BugFixes        int                   `json:"bug_fixes"`
	Reflections     int                   `json:"reflections"`
	GitCommits      int                   `json:"git_commits"`
	TotalCheckins   int                   `json:"total_checkins"`
	ProductiveWeeks int                   `json:"productive_weeks"`
	Mission         string                `json:"mission"`
	Achievements    []AchievementProgress `json:"achievements"`
}

type App struct {
	mu       sync.Mutex
	state    AppState
	dataPath string
}

var achievementDefs = []AchievementDef{
	{
		ID:          "habit_streak",
		Name:        "连续学习",
		Category:    "习惯",
		Description: "连续打卡 3/7/30 天",
		Metric:      "streak",
		Tiers: []TierDef{
			{Name: "铜", Target: 3},
			{Name: "银", Target: 7},
			{Name: "金", Target: 30},
		},
	},
	{
		ID:          "habit_total_checkin",
		Name:        "学习常驻",
		Category:    "习惯",
		Description: "累计打卡 10/50/120 天",
		Metric:      "total_checkins",
		Tiers: []TierDef{
			{Name: "铜", Target: 10},
			{Name: "银", Target: 50},
			{Name: "金", Target: 120},
		},
	},
	{
		ID:          "habit_productive_week",
		Name:        "高效周",
		Category:    "习惯",
		Description: "每周至少 5 次学习，累计完成 1/4/12 周",
		Metric:      "productive_weeks",
		Tiers: []TierDef{
			{Name: "铜", Target: 1},
			{Name: "银", Target: 4},
			{Name: "金", Target: 12},
		},
	},
	{
		ID:          "skill_modules",
		Name:        "技能通关",
		Category:    "技能",
		Description: "完成技能模块 3/8/15 个",
		Metric:      "skill_modules",
		Tiers: []TierDef{
			{Name: "铜", Target: 3},
			{Name: "银", Target: 8},
			{Name: "金", Target: 15},
		},
	},
	{
		ID:          "skill_xp",
		Name:        "经验成长",
		Category:    "技能",
		Description: "累计经验值达到 200/800/2000",
		Metric:      "xp",
		Tiers: []TierDef{
			{Name: "铜", Target: 200},
			{Name: "银", Target: 800},
			{Name: "金", Target: 2000},
		},
	},
	{
		ID:          "project_delivery",
		Name:        "作品落地",
		Category:    "作品",
		Description: "完成项目里程碑 1/3/6 次",
		Metric:      "projects",
		Tiers: []TierDef{
			{Name: "铜", Target: 1},
			{Name: "银", Target: 3},
			{Name: "金", Target: 6},
		},
	},
	{
		ID:          "challenge_bugfix",
		Name:        "Bug 猎手",
		Category:    "挑战",
		Description: "完成 Bug 修复 5/20/50 次",
		Metric:      "bug_fixes",
		Tiers: []TierDef{
			{Name: "铜", Target: 5},
			{Name: "银", Target: 20},
			{Name: "金", Target: 50},
		},
	},
	{
		ID:          "challenge_reflection",
		Name:        "复盘者",
		Category:    "挑战",
		Description: "完成学习复盘 3/10/30 次",
		Metric:      "reflections",
		Tiers: []TierDef{
			{Name: "铜", Target: 3},
			{Name: "银", Target: 10},
			{Name: "金", Target: 30},
		},
	},
	{
		ID:          "collab_git",
		Name:        "协作节奏",
		Category:    "协作",
		Description: "累计 Git 提交 10/50/200 次",
		Metric:      "git_commits",
		Tiers: []TierDef{
			{Name: "铜", Target: 10},
			{Name: "银", Target: 50},
			{Name: "金", Target: 200},
		},
	},
}

func defaultState() AppState {
	return AppState{
		CheckinDates:   map[string]bool{},
		WeeklyCheckins: map[string]int{},
	}
}

func (s *AppState) normalize() {
	if s.CheckinDates == nil {
		s.CheckinDates = map[string]bool{}
	}
	if s.WeeklyCheckins == nil {
		s.WeeklyCheckins = map[string]int{}
	}
}

func newApp(dataPath string) (*App, error) {
	app := &App{
		state:    defaultState(),
		dataPath: dataPath,
	}
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		return nil, err
	}
	if err := app.load(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) load() error {
	data, err := os.ReadFile(a.dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &a.state); err != nil {
		return err
	}
	a.state.normalize()
	return nil
}

func (a *App) saveLocked() error {
	payload, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.dataPath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.dataPath)
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	writeJSON(w, http.StatusOK, a.dashboardLocked(time.Now()))
}

func (a *App) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	day := isoDate(now)
	if a.state.CheckinDates[day] {
		writeError(w, http.StatusConflict, "今天已经打卡过了")
		return
	}

	a.state.CheckinDates[day] = true
	a.state.WeeklyCheckins[isoWeekKey(now)]++
	a.state.XP += 20
	a.state.UpdatedAt = now

	if err := a.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "保存数据失败")
		return
	}
	writeJSON(w, http.StatusOK, a.dashboardLocked(now))
}

func (a *App) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Amount == 0 {
		req.Amount = 1
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.applyActionLocked(req.Kind, req.Amount); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "保存数据失败")
		return
	}
	writeJSON(w, http.StatusOK, a.dashboardLocked(time.Now()))
}

func (a *App) applyActionLocked(kind string, amount int) error {
	if amount < 1 {
		return errors.New("amount 必须大于 0")
	}
	if amount > 100 {
		return errors.New("amount 过大")
	}

	switch kind {
	case "study_hour":
		a.state.StudyHours += amount
		a.state.XP += amount * 15
	case "skill_module":
		a.state.SkillModules += amount
		a.state.XP += amount * 35
	case "project":
		a.state.Projects += amount
		a.state.XP += amount * 100
	case "bug_fix":
		a.state.BugFixes += amount
		a.state.XP += amount * 25
	case "reflection":
		a.state.Reflections += amount
		a.state.XP += amount * 20
	case "git_commit":
		a.state.GitCommits += amount
		a.state.XP += amount * 8
	default:
		return fmt.Errorf("未知动作类型: %s", kind)
	}

	a.state.UpdatedAt = time.Now()
	return nil
}

func (a *App) dashboardLocked(now time.Time) DashboardResponse {
	streak := calculateStreak(a.state.CheckinDates, now)
	totalCheckins := len(a.state.CheckinDates)
	productiveWeeks := countProductiveWeeks(a.state.WeeklyCheckins)

	level := a.state.XP/levelStepXP + 1
	levelFloor := (level - 1) * levelStepXP
	levelCeiling := level * levelStepXP

	achievements := make([]AchievementProgress, 0, len(achievementDefs))
	for _, def := range achievementDefs {
		value := metricValue(a.state, def.Metric, now)
		currentTier := 0
		for i, tier := range def.Tiers {
			if value >= tier.Target {
				currentTier = i + 1
			}
		}

		tierName := "未解锁"
		if currentTier > 0 {
			tierName = def.Tiers[currentTier-1].Name
		}

		nextTarget := 0
		progress := 1.0
		if currentTier < len(def.Tiers) {
			nextTarget = def.Tiers[currentTier].Target
			progress = float64(value) / float64(nextTarget)
			if progress > 1 {
				progress = 1
			}
		}

		achievements = append(achievements, AchievementProgress{
			ID:          def.ID,
			Name:        def.Name,
			Category:    def.Category,
			Description: def.Description,
			Value:       value,
			CurrentTier: currentTier,
			TierName:    tierName,
			NextTarget:  nextTarget,
			Progress:    progress,
		})
	}

	sort.Slice(achievements, func(i, j int) bool {
		if achievements[i].Category == achievements[j].Category {
			return achievements[i].Name < achievements[j].Name
		}
		return achievements[i].Category < achievements[j].Category
	})

	return DashboardResponse{
		XP:              a.state.XP,
		Level:           level,
		CurrentLevelXP:  a.state.XP - levelFloor,
		NextLevelXP:     levelCeiling - levelFloor,
		Streak:          streak,
		StudyHours:      a.state.StudyHours,
		SkillModules:    a.state.SkillModules,
		Projects:        a.state.Projects,
		BugFixes:        a.state.BugFixes,
		Reflections:     a.state.Reflections,
		GitCommits:      a.state.GitCommits,
		TotalCheckins:   totalCheckins,
		ProductiveWeeks: productiveWeeks,
		Mission:         recommendMission(achievements),
		Achievements:    achievements,
	}
}

func metricValue(state AppState, metric string, now time.Time) int {
	switch metric {
	case "streak":
		return calculateStreak(state.CheckinDates, now)
	case "total_checkins":
		return len(state.CheckinDates)
	case "productive_weeks":
		return countProductiveWeeks(state.WeeklyCheckins)
	case "skill_modules":
		return state.SkillModules
	case "xp":
		return state.XP
	case "projects":
		return state.Projects
	case "bug_fixes":
		return state.BugFixes
	case "reflections":
		return state.Reflections
	case "git_commits":
		return state.GitCommits
	default:
		return 0
	}
}

func recommendMission(achievements []AchievementProgress) string {
	best := AchievementProgress{}
	found := false
	bestScore := -1.0
	bestRemaining := 1 << 30

	for _, item := range achievements {
		if item.NextTarget == 0 {
			continue
		}
		remaining := item.NextTarget - item.Value
		score := item.Progress
		if !found || score > bestScore || (score == bestScore && remaining < bestRemaining) {
			best = item
			found = true
			bestScore = score
			bestRemaining = remaining
		}
	}

	if !found {
		return "主线任务：全部成就已达成，去做一个完整上线项目。"
	}
	return fmt.Sprintf("主线任务：推进「%s」，还差 %d", best.Name, bestRemaining)
}

func countProductiveWeeks(weekly map[string]int) int {
	count := 0
	for _, value := range weekly {
		if value >= 5 {
			count++
		}
	}
	return count
}

func calculateStreak(checkins map[string]bool, now time.Time) int {
	if len(checkins) == 0 {
		return 0
	}

	anchor := dateOnly(now)
	if !checkins[isoDate(anchor)] {
		anchor = anchor.AddDate(0, 0, -1)
	}

	streak := 0
	for checkins[isoDate(anchor)] {
		streak++
		anchor = anchor.AddDate(0, 0, -1)
	}
	return streak
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func isoDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func isoWeekKey(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func main() {
	app, err := newApp(filepath.Join("data", "state.json"))
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/api/state", app.handleState)
	mux.HandleFunc("/api/checkin", app.handleCheckin)
	mux.HandleFunc("/api/action", app.handleAction)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("achievement system is running at http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}
