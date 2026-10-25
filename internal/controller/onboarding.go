package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

// The initialization guide is Web presentation state: which steps an
// administrator has started, skipped or confirmed. It never changes desired
// runtime state, so it lives in app_settings (outside the configuration
// revision tables) and is served only on the /api/v1/ui surface.
const onboardingSetupSetting = "onboarding.setup"

const (
	onboardingStatusPending    = "pending"
	onboardingStatusInProgress = "in_progress"
	onboardingStatusCompleted  = "completed"
	onboardingStatusDismissed  = "dismissed"
)

type onboardingState struct {
	Status          string     `json:"status"`
	PasswordRotated bool       `json:"password_rotated"`
	Skipped         []string   `json:"skipped"`
	Confirmed       []string   `json:"confirmed"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	DismissedAt     *time.Time `json:"dismissed_at,omitempty"`
}

type onboardingItemSpec struct {
	key         string
	level       string
	skippable   bool
	confirmable bool
}

// onboardingItems is the checklist in display order. Required items must be
// done or skipped before the guide can be completed; recommended items never
// block it.
var onboardingItems = []onboardingItemSpec{
	{key: "password", level: "required"},
	{key: "controller_url", level: "required"},
	// AgentLink must be running before any Agent install command can be
	// issued, so it cannot be skipped either.
	{key: "agent_link", level: "required"},
	{key: "node_defaults", level: "required", skippable: true, confirmable: true},
	{key: "server", level: "required", skippable: true},
	{key: "entry", level: "required", skippable: true},
	{key: "access", level: "required", skippable: true},
	{key: "subscription", level: "required", skippable: true, confirmable: true},
	{key: "two_factor", level: "recommended", skippable: true},
	{key: "backup", level: "recommended", skippable: true},
	{key: "notifications", level: "recommended", skippable: true},
	{key: "dns_credentials", level: "recommended", skippable: true},
	{key: "updates", level: "recommended", skippable: true, confirmable: true},
	{key: "audit", level: "recommended", skippable: true, confirmable: true},
	{key: "security", level: "recommended", skippable: true, confirmable: true},
	{key: "retention", level: "recommended", skippable: true, confirmable: true},
}

var onboardingTourKeys = []string{"proxy_path"}

type onboardingItem struct {
	Key    string         `json:"key"`
	Level  string         `json:"level"`
	Status string         `json:"status"`
	Detail map[string]any `json:"detail,omitempty"`
}

type onboardingView struct {
	State          onboardingState   `json:"state"`
	Mode           string            `json:"mode"`
	ShowWelcome    bool              `json:"show_welcome"`
	Items          []onboardingItem  `json:"items"`
	RequiredTotal  int               `json:"required_total"`
	RequiredDone   int               `json:"required_done"`
	CanComplete    bool              `json:"can_complete"`
	Tours          map[string]string `json:"tours"`
	BootstrapAdmin bool              `json:"bootstrap_admin"`
}

func (s *Server) loadOnboardingState(ctx context.Context) (onboardingState, bool, error) {
	raw, err := s.store.GetSetting(ctx, onboardingSetupSetting)
	if err != nil || strings.TrimSpace(raw) == "" {
		return onboardingState{Status: onboardingStatusPending}, false, err
	}
	var state onboardingState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return onboardingState{Status: onboardingStatusPending}, false, nil
	}
	if state.Status == "" {
		state.Status = onboardingStatusPending
	}
	return state, true, nil
}

func (s *Server) saveOnboardingState(ctx context.Context, state onboardingState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, onboardingSetupSetting, string(raw))
}

// onboardingFresh decides whether an untracked installation is new. It is
// derived on read and never written back, so upgrading an established panel
// or restoring an older backup needs no state migration.
func onboardingFresh(signals store.OnboardingSignals) bool {
	return signals.Servers == 0 && signals.EnabledInbounds == 0 && signals.Users <= 1
}

func (s *Server) onboardingView(ctx context.Context, user *model.User) (onboardingView, error) {
	state, stored, err := s.loadOnboardingState(ctx)
	if err != nil {
		return onboardingView{}, err
	}
	signals, err := s.store.OnboardingSignals(ctx, user.ID)
	if err != nil {
		return onboardingView{}, err
	}
	settings, err := s.store.ListSettings(ctx)
	if err != nil {
		return onboardingView{}, err
	}
	bootstrap, err := s.store.IsBootstrapAdmin(ctx, user.ID)
	if err != nil {
		return onboardingView{}, err
	}
	tours, err := s.store.OnboardingTours(ctx, user.ID)
	if err != nil {
		return onboardingView{}, err
	}
	mode := "tracked"
	if !stored {
		mode = "established"
		if onboardingFresh(signals) {
			mode = "fresh"
		}
	}
	controllerURL := strings.TrimSpace(settings["controller_url"])
	agentLink := s.publicStealthSettings(settings)
	_, _, agentLinkActive := s.StealthTransportInfo()
	done := map[string]bool{
		// Only the bootstrap account starts from a generated password; other
		// administrators and established panels have nothing to rotate.
		"password":        state.PasswordRotated || !bootstrap || mode == "established",
		"controller_url":  controllerURL != "",
		"agent_link":      agentLinkActive,
		"node_defaults":   slices.Contains(state.Confirmed, "node_defaults") || signals.Servers > 0,
		"server":          signals.OnlineServers > 0,
		"entry":           signals.EnabledInbounds > 0,
		"access":          signals.PlansWithNodes > 0 && signals.ActivePlanBindings > 0,
		"subscription":    slices.Contains(state.Confirmed, "subscription"),
		"two_factor":      signals.SecondFactor,
		"backup":          settingEnabledValue(settings[controllerBackupEnabledSetting]),
		"notifications":   signals.NotificationChannels > 0,
		"dns_credentials": signals.DNSCredentials > 0,
	}
	details := map[string]map[string]any{
		"controller_url": {"url": controllerURL, "https": strings.HasPrefix(strings.ToLower(controllerURL), "https://")},
		"agent_link":     agentLink,
		"server":         {"servers": signals.Servers, "online": signals.OnlineServers},
		"entry":          {"inbounds": signals.EnabledInbounds},
		"access":         {"plans": signals.PlansWithNodes, "bindings": signals.ActivePlanBindings},
	}
	view := onboardingView{State: state, Mode: mode, Tours: tours, BootstrapAdmin: bootstrap}
	if view.State.Skipped == nil {
		view.State.Skipped = []string{}
	}
	if view.State.Confirmed == nil {
		view.State.Confirmed = []string{}
	}
	view.CanComplete = true
	for _, spec := range onboardingItems {
		status := "pending"
		switch {
		case done[spec.key] || (spec.confirmable && slices.Contains(state.Confirmed, spec.key)):
			status = "done"
		case slices.Contains(state.Skipped, spec.key):
			status = "skipped"
		}
		view.Items = append(view.Items, onboardingItem{Key: spec.key, Level: spec.level, Status: status, Detail: details[spec.key]})
		if spec.level == "required" {
			view.RequiredTotal++
			if status == "done" {
				view.RequiredDone++
			}
			if status == "pending" {
				view.CanComplete = false
			}
		}
	}
	view.ShowWelcome = state.Status == onboardingStatusPending && (stored || mode == "fresh")
	return view, nil
}

func settingEnabledValue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func onboardingSpec(key string) (onboardingItemSpec, bool) {
	for _, spec := range onboardingItems {
		if spec.key == key {
			return spec, true
		}
	}
	return onboardingItemSpec{}, false
}

func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		fail(w, errors.New("invalid session"), http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
			Item   string `json:"item"`
		}
		if !decode(w, r, &req) {
			return
		}
		if status, err := s.applyOnboardingAction(r.Context(), user, req.Action, req.Item); err != nil {
			fail(w, err, status)
			return
		}
	default:
		method(w)
		return
	}
	view, err := s.onboardingView(r.Context(), user)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	write(w, http.StatusOK, view)
}

func (s *Server) applyOnboardingAction(ctx context.Context, user *model.User, action, item string) (int, error) {
	state, _, err := s.loadOnboardingState(ctx)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	now := time.Now().UTC()
	switch action {
	case "start":
		if state.Status != onboardingStatusCompleted {
			state.Status = onboardingStatusInProgress
		}
		if state.StartedAt == nil {
			state.StartedAt = &now
		}
	case "dismiss":
		if state.Status == onboardingStatusPending {
			state.Status = onboardingStatusDismissed
			state.DismissedAt = &now
		}
	case "skip", "unskip", "confirm", "unconfirm":
		spec, ok := onboardingSpec(item)
		if !ok {
			return http.StatusBadRequest, errors.New("unknown onboarding item")
		}
		switch action {
		case "skip":
			if !spec.skippable {
				return http.StatusBadRequest, errors.New("this onboarding item cannot be skipped")
			}
			state.Skipped = appendUnique(state.Skipped, item)
		case "unskip":
			state.Skipped = slices.DeleteFunc(state.Skipped, func(value string) bool { return value == item })
		case "confirm":
			if !spec.confirmable {
				return http.StatusBadRequest, errors.New("this onboarding item is completed by its own data")
			}
			state.Confirmed = appendUnique(state.Confirmed, item)
			state.Skipped = slices.DeleteFunc(state.Skipped, func(value string) bool { return value == item })
		case "unconfirm":
			state.Confirmed = slices.DeleteFunc(state.Confirmed, func(value string) bool { return value == item })
		}
		if state.Status == onboardingStatusPending || state.Status == onboardingStatusDismissed {
			state.Status = onboardingStatusInProgress
		}
	case "complete":
		// Completion is checked against the same derived checklist the
		// guide shows, so it cannot be completed with a required item open.
		view, err := s.onboardingView(ctx, user)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !view.CanComplete {
			return http.StatusConflict, errors.New("required onboarding items are still open")
		}
		state.Status = onboardingStatusCompleted
		state.CompletedAt = &now
	case "reopen":
		state.Status = onboardingStatusInProgress
		state.CompletedAt = nil
	default:
		return http.StatusBadRequest, errors.New("unknown onboarding action")
	}
	if err := s.saveOnboardingState(ctx, state); err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, nil
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func (s *Server) onboardingTours(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		method(w)
		return
	}
	user := currentUser(r)
	if user == nil {
		fail(w, errors.New("invalid session"), http.StatusUnauthorized)
		return
	}
	var req struct {
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !slices.Contains(onboardingTourKeys, req.Key) || (req.Status != "started" && req.Status != "completed") {
		fail(w, errors.New("unknown onboarding tour or status"), http.StatusBadRequest)
		return
	}
	if err := s.store.SetOnboardingTour(r.Context(), user.ID, req.Key, req.Status); err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	tours, err := s.store.OnboardingTours(r.Context(), user.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	write(w, http.StatusOK, map[string]any{"tours": tours})
}

// recordOnboardingPasswordRotation marks the generated bootstrap password as
// replaced. Established panels without guide state are left untouched so a
// routine password change never makes the guide appear.
func (s *Server) recordOnboardingPasswordRotation(ctx context.Context, userID int64) error {
	bootstrap, err := s.store.IsBootstrapAdmin(ctx, userID)
	if err != nil || !bootstrap {
		return err
	}
	state, stored, err := s.loadOnboardingState(ctx)
	if err != nil {
		return err
	}
	if !stored {
		signals, err := s.store.OnboardingSignals(ctx, userID)
		if err != nil || !onboardingFresh(signals) {
			return err
		}
	}
	if state.PasswordRotated {
		return nil
	}
	state.PasswordRotated = true
	return s.saveOnboardingState(ctx, state)
}
