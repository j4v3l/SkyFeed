---
layout: default
title: Access and moderation
---

# Access and moderation

SkyFeed combines project-owned access tiers with Discord's native permissions
and role hierarchy. It does not create roles and it never treats a command's
visibility in the client as authorization.

## Access tiers

Viewer access covers read-only aircraft, weather, feeder, and help views.
Operator, Moderator, and Admin tiers are bound to existing Discord roles with
protected `/settings roles` commands. A server owner or member with Discord's
Administrator permission provides the initial bootstrap.

- Operators manage alerts, reports, and server watch rules with Manage Server.
- Moderators use moderation commands with the matching native Discord
  permission and a valid role hierarchy.
- Admins manage SkyFeed settings with Manage Server. Role-binding changes also
  require Manage Roles.

The bot itself should receive only the permissions it needs: View Channels,
Send Messages, Embed Links, Read Message History, Moderate Members, Kick
Members, and Ban Members. Attach Files is optional. Do not grant Administrator,
Manage Roles, Message Content, or privileged member intents.

## Moderation records

SkyFeed supports warnings, timeouts, timeout removal, kicks, bans, unbans, case
lookup, and bounded case history. Destructive actions require a private,
invoker-bound confirmation that expires after 60 seconds.

A durable case records the moderator, target, reason, time, outcome, and warning
DM status. Moderation-log delivery uses a bounded SQLite outbox so a Discord
delivery failure cannot undo a completed action. Cases are retained for 365
days and then removed in bounded batches.

## Privacy and accountability

Allowed mentions default to none. Administrative responses are ephemeral, and
the bot rechecks guild, user, role, permission, and hierarchy on every component
interaction. Server owners remain responsible for staff selection, action
reasons, appeals, and compliance with local rules and law.

Public policies:

- [Terms of Service](legal/terms.md)
- [Privacy Policy](legal/privacy.md)

Privacy, deletion, and support requests may be sent to `jj4v3l@gmail.com`.
