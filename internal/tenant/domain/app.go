package domain

import "time"

// AppStatus is the soft lifecycle state of a tenant app.
type AppStatus string

const (
	AppStatusActive   AppStatus = "active"
	AppStatusDisabled AppStatus = "disabled"
)

// App is the Tenant primary entity.
type App struct {
	AppID         string         `json:"app_id"`
	AppSecretHash string         `json:"-"`
	AppName       string         `json:"app_name"`
	RedirectURIs  []string       `json:"redirect_uris"`
	Status        AppStatus      `json:"status"`
	Settings      map[string]any `json:"settings"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func (a *App) IsActive() bool {
	return a != nil && a.Status == AppStatusActive
}

// Clone returns a shallow copy with copied slices/maps.
func (a *App) Clone() *App {
	if a == nil {
		return nil
	}
	out := *a
	if a.RedirectURIs != nil {
		out.RedirectURIs = append([]string(nil), a.RedirectURIs...)
	}
	if a.Settings != nil {
		out.Settings = make(map[string]any, len(a.Settings))
		for k, v := range a.Settings {
			out.Settings[k] = v
		}
	}
	return &out
}
