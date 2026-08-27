package render

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
)

const (
	maxTitle       = 256
	maxDescription = 4096
	maxFieldName   = 256
	maxFieldValue  = 1024
	maxFooter      = 2048
	maxFields      = 25
	maxEmbedText   = 6000
)

var plainTextReplacer = strings.NewReplacer(
	"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "~", "\\~", "|", "\\|",
	">", "\\>", "#", "\\#", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
	"https://", "https[:]//", "http://", "http[:]//",
)

func Truncate(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

// PlainText neutralizes Discord markdown and user-supplied links before a
// value is placed in an embed or moderation DM. Allowed mentions remain
// disabled separately on every message.
func PlainText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = plainTextReplacer.Replace(value)
	return value
}

// InlineCode preserves trusted operational values such as enrollment URLs
// while preventing them from escaping a Discord inline-code span.
func InlineCode(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '`' || (unicode.IsControl(r) && r != '\t') {
			return -1
		}
		return r
	}, value)
	return "`" + value + "`"
}

// BoundEmbed applies Discord's documented per-field and aggregate text limits.
// It intentionally drops overflow fields instead of creating an invalid payload.
func BoundEmbed(embed discord.Embed) discord.Embed {
	embed.Title = Truncate(embed.Title, maxTitle)
	embed.Description = Truncate(embed.Description, maxDescription)
	if embed.Footer != nil {
		embed.Footer.Text = Truncate(embed.Footer.Text, maxFooter)
	}
	if len(embed.Fields) > maxFields {
		embed.Fields = embed.Fields[:maxFields]
	}

	used := runeCount(embed.Title) + runeCount(embed.Description)
	if embed.Footer != nil {
		embed.Footer.Text = Truncate(embed.Footer.Text, maxEmbedText-used)
		used += runeCount(embed.Footer.Text)
	}
	fields := make([]discord.EmbedField, 0, len(embed.Fields))
	for _, field := range embed.Fields {
		// Inline columns reflow unpredictably between Discord desktop panes and
		// mobile clients. SkyFeed uses one universal, full-width payload.
		inline := false
		field.Inline = &inline
		field.Name = Truncate(strings.TrimSpace(field.Name), maxFieldName)
		field.Value = Truncate(strings.TrimSpace(field.Value), maxFieldValue)
		need := runeCount(field.Name) + runeCount(field.Value)
		if used+need > maxEmbedText {
			remaining := maxEmbedText - used - runeCount(field.Name)
			if remaining <= 0 {
				break
			}
			field.Value = Truncate(field.Value, remaining)
			need = runeCount(field.Name) + runeCount(field.Value)
		}
		fields = append(fields, field)
		used += need
	}
	embed.Fields = fields
	return embed
}

func runeCount(value string) int { return utf8.RuneCountInString(value) }
