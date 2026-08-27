package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

const DefaultPageSize = 5

// CardModel is the Discord-independent presentation contract used by adapters
// that need a compact card without owning Discord layout details.
type CardModel struct {
	View      string
	Status    string
	Purpose   string
	Color     int
	Timestamp time.Time
	Sections  []FactGroup
	Footer    string
}

type FactGroup struct {
	Title string
	Lines []string
}

func Card(model CardModel) discord.Embed {
	color := model.Color
	if color == 0 {
		color = Scope
	}
	description := strings.TrimSpace(model.Status)
	if purpose := strings.TrimSpace(model.Purpose); purpose != "" {
		if description != "" {
			description += "\n"
		}
		description += purpose
	}
	embed := base(valueOr(model.View, "Update"), color, model.Timestamp).WithDescription(description)
	for _, group := range model.Sections {
		lines := make([]string, 0, len(group.Lines))
		for _, line := range group.Lines {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			embed.Fields = append(embed.Fields, section(group.Title, strings.Join(lines, "\n")))
		}
	}
	if footer := strings.TrimSpace(model.Footer); footer != "" {
		embed.Footer = &discord.EmbedFooter{Text: PlainText(footer)}
	}
	return BoundEmbed(embed)
}

// Facts groups a small number of related facts per line. Discord chooses the
// actual line wrapping for each client; semantic breaks keep the same payload
// readable on narrow mobile screens and wide desktop panes.
func Facts(values ...string) string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return "Unavailable"
	}
	lines := make([]string, 0, (len(clean)+2)/3)
	for start := 0; start < len(clean); start += 3 {
		lines = append(lines, strings.Join(clean[start:min(start+3, len(clean))], " · "))
	}
	return strings.Join(lines, "\n")
}

func Labeled(label, value string) string {
	return "**" + PlainText(strings.TrimSpace(label)) + "** " + PlainText(strings.TrimSpace(value))
}

func labeledMarkup(label, value string) string {
	return "**" + PlainText(strings.TrimSpace(label)) + "** " + strings.TrimSpace(value)
}

func RelativeTime(value time.Time, fallback string) string {
	if value.IsZero() {
		return fallback
	}
	return fmt.Sprintf("<t:%d:R>", value.Unix())
}

func PageBounds(total, page, pageSize int) (start, end, normalizedPage, maxPage int) {
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	maxPage = max(0, (total-1)/pageSize)
	normalizedPage = min(max(page, 0), maxPage)
	start = min(normalizedPage*pageSize, total)
	end = min(start+pageSize, total)
	return
}

func PageDescription(noun string, total, start, end, page, maxPage int) string {
	if total == 0 {
		return "No " + noun + " are available in this view."
	}
	return fmt.Sprintf("Showing %d–%d of %d %s\nPage %d of %d", start+1, end, total, noun, page+1, maxPage+1)
}

func Info(view, description string, now time.Time) discord.Embed {
	return BoundEmbed(base(view, Radar, now).WithDescription("🟢 **DONE**\n" + PlainText(description)))
}

// InfoMarkup renders presentation-owned markup. Callers must sanitize every
// user-controlled fragment before constructing description.
func InfoMarkup(view, description string, now time.Time) discord.Embed {
	return BoundEmbed(base(view, Radar, now).WithDescription("🟢 **DONE**\n" + description))
}

func Error(description string, now time.Time) discord.Embed {
	return BoundEmbed(base("Error", Caution, now).
		WithDescription("🟡 **NEEDS ATTENTION**\n" + PlainText(description)))
}

type FeederListItem struct {
	Name     string
	Area     string
	State    string
	Aircraft int
}

func FeedersPage(items []FeederListItem, page, pageSize int, now time.Time) discord.Embed {
	start, end, page, maxPage := PageBounds(len(items), page, pageSize)
	embed := base("Community feeders", Scope, now).
		WithDescription("Approved community coverage only. Account, network, and receiver details stay hidden.\n" +
			PageDescription("feeders", len(items), start, end, page, maxPage))
	for index, item := range items[start:end] {
		state := strings.ToLower(strings.TrimSpace(item.State))
		badge := "⚪"
		switch state {
		case "healthy":
			badge = "🟢"
		case "stale", "degraded":
			badge = "🟡"
		case "offline":
			badge = "🔴"
		}
		embed.Fields = append(embed.Fields, section(
			fmt.Sprintf("%d. %s %s", start+index+1, badge, PlainText(valueOr(item.Name, "Unnamed feeder"))),
			Facts(Labeled("Area", valueOr(item.Area, "Not configured")), Labeled("Status", valueOr(state, "unknown")), fmt.Sprintf("**Aircraft** %d", item.Aircraft)),
		))
	}
	return BoundEmbed(embed)
}

func WatchRulesPage(rules []domain.WatchRule, page, pageSize int, now time.Time) discord.Embed {
	start, end, page, maxPage := PageBounds(len(rules), page, pageSize)
	embed := base("Watch rules", Scope, now).WithDescription(PageDescription("watch rules", len(rules), start, end, page, maxPage))
	for _, rule := range rules[start:end] {
		state, icon := "Disabled", "⚪"
		if rule.Enabled {
			state, icon = "Enabled", "🟢"
		}
		scope := "Personal"
		if rule.ServerScope {
			scope = "Server"
		}
		embed.Fields = append(embed.Fields, section(
			fmt.Sprintf("%s Rule #%d • %s", icon, rule.ID, PlainText(string(rule.Type))),
			Facts("`"+PlainText(rule.Value)+"`", Labeled("Scope", scope), Labeled("State", state), Labeled("Feeder", valueOr(string(rule.FeederScope), "all"))),
		))
	}
	return BoundEmbed(embed)
}

func AlertConfigsPage(values []storage.AlertConfig, page, pageSize int, now time.Time) discord.Embed {
	start, end, page, maxPage := PageBounds(len(values), page, pageSize)
	embed := base("Alert settings", Scope, now).WithDescription(PageDescription("alert categories", len(values), start, end, page, maxPage))
	if len(values) == 0 {
		embed.Description = "Safe alert defaults are active. No category overrides are configured."
	}
	for _, value := range values[start:end] {
		state, icon := "Disabled", "⚪"
		if value.Enabled {
			state, icon = "Enabled", "🟢"
		}
		destination := "Default channel"
		if value.Destination != 0 {
			destination = fmt.Sprintf("<#%d>", value.Destination)
		}
		embed.Fields = append(embed.Fields, section(
			icon+" "+PlainText(value.Category),
			Facts(Labeled("State", state), Labeled("Cooldown", conciseDuration(value.Cooldown)), labeledMarkup("Destination", destination)),
		))
	}
	return BoundEmbed(embed)
}

func ReportSchedulesPage(values []storage.ReportSchedule, page, pageSize int, now time.Time) discord.Embed {
	start, end, page, maxPage := PageBounds(len(values), page, pageSize)
	embed := base("Report schedules", Scope, now).WithDescription(PageDescription("schedules", len(values), start, end, page, maxPage))
	for _, value := range values[start:end] {
		state, icon := "Disabled", "⚪"
		if value.Enabled {
			state, icon = "Enabled", "🟢"
		}
		embed.Fields = append(embed.Fields, section(
			fmt.Sprintf("%s Schedule #%d • %s", icon, value.ID, PlainText(value.Cadence)),
			Facts(Labeled("State", state), fmt.Sprintf("**Channel** <#%d>", value.Destination), labeledMarkup("Last run", RelativeTime(value.LastRun, "Never"))),
		))
	}
	return BoundEmbed(embed)
}

func ModerationHistoryPage(values []storage.ModerationCase, page, pageSize int, now time.Time) discord.Embed {
	start, end, page, maxPage := PageBounds(len(values), page, pageSize)
	embed := base("Moderation history", Scope, now).WithDescription(PageDescription("cases", len(values), start, end, page, maxPage))
	for _, value := range values[start:end] {
		icon := "🟢"
		if value.Status == "failed" {
			icon = "🔴"
		} else if value.Status == "pending" {
			icon = "🟡"
		}
		embed.Fields = append(embed.Fields, section(
			fmt.Sprintf("%s Case %d • %s", icon, value.ID, PlainText(strings.ToUpper(value.Action))),
			Facts(fmt.Sprintf("**User** `%d`", value.TargetUserID), Labeled("Status", strings.ToUpper(value.Status)), labeledMarkup("Created", RelativeTime(value.CreatedAt, "Unknown")))+"\n"+
				"**Reason**\n"+Truncate(PlainText(value.Reason), 220),
		))
	}
	return BoundEmbed(embed)
}
