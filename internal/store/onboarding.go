package store

import (
	"context"
	"time"
)

// OnboardingSignals is the live evidence behind the initialization checklist.
// Every item is derived from these counts on each read, so the checklist can
// never disagree with the data it describes.
type OnboardingSignals struct {
	Servers              int
	OnlineServers        int
	EnabledInbounds      int
	Users                int
	PlansWithNodes       int
	ActivePlanBindings   int
	NotificationChannels int
	DNSCredentials       int
	SecondFactor         bool
}

func (s *Store) OnboardingSignals(ctx context.Context, userID int64) (OnboardingSignals, error) {
	var out OnboardingSignals
	var secondFactor int
	err := s.db.QueryRowContext(ctx, `select
		(select count(*) from servers),
		(select count(*) from servers where status='online'),
		(select count(*) from inbounds where enabled=1),
		(select count(*) from users),
		(select count(distinct p.id) from subscription_plans p join subscription_plan_revision_nodes n on n.revision_id=p.current_revision_id where p.enabled=1),
		(select count(*) from user_plan_bindings where enabled=1 and status in ('active','pending')),
		(select count(*) from notification_channels where enabled=1),
		(select count(*) from dns_credentials),
		(select (exists(select 1 from user_authentication where user_id=? and totp_enabled=1) or exists(select 1 from passkey_credentials where user_id=?)))`,
		userID, userID,
	).Scan(&out.Servers, &out.OnlineServers, &out.EnabledInbounds, &out.Users, &out.PlansWithNodes, &out.ActivePlanBindings, &out.NotificationChannels, &out.DNSCredentials, &secondFactor)
	out.SecondFactor = secondFactor != 0
	return out, err
}

// OnboardingTours returns one user's tutorial progress keyed by tour.
func (s *Store) OnboardingTours(ctx context.Context, userID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `select tour_key,status from user_onboarding_tours where user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, status string
		if err := rows.Scan(&key, &status); err != nil {
			return nil, err
		}
		out[key] = status
	}
	return out, rows.Err()
}

func (s *Store) SetOnboardingTour(ctx context.Context, userID int64, key, status string) error {
	_, err := s.db.ExecContext(ctx, `insert into user_onboarding_tours(user_id,tour_key,status,updated_at) values(?,?,?,?)
		on conflict(user_id,tour_key) do update set status=excluded.status,updated_at=excluded.updated_at`, userID, key, status, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
