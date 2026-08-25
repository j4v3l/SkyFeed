package health

import "time"

// View is a Discord-safe copy of process health for admin audit embeds.
type View struct {
	Status     string
	Live       bool
	Ready      bool
	Uptime     time.Duration
	Components map[string]Component
}

// View returns the current health snapshot without privacy disclosure details.
func (s *State) View(now time.Time) View {
	snap := s.Snapshot(now)
	return View{
		Status:     snap.Status,
		Live:       snap.Live,
		Ready:      snap.Ready,
		Uptime:     time.Duration(snap.UptimeSeconds * float64(time.Second)),
		Components: snap.Components,
	}
}
