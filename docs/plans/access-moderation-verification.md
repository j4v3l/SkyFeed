# Access, moderation, and Discord verification plan

Status: role access and moderation implemented and tested; public legal pages
published and validated over HTTPS.

This document records decisions derived during design work without retaining
chat transcripts, credentials, private receiver addresses, or Discord server
identifiers.

## Role-based access

- Keep read-only aircraft commands available to everyone.
- Bind existing Discord roles through protected `/settings roles` commands;
  SkyFeed must not create roles or require `Manage Roles`.
- Use Viewer, Operator, Moderator, and Admin tiers. Viewer access is implicit
  for public commands; the other three bindings are durable guild settings.
- A server owner or member with `Administrator` remains the bootstrap path.
- Moderators must have both the configured Moderator-or-Admin tier and the
  native Discord permission required for the action. Bot and invoker role
  hierarchy checks remain mandatory.

## Moderation

- Add warn, timeout, remove-timeout, kick, ban, unban, case lookup, and bounded
  case-history commands.
- Require an actionable reason and create a durable case before calling
  Discord. Record success, failure, DM delivery, moderator, target, and time.
- Require a private, invoker-bound, 60-second confirmation for kicks and bans.
- Deliver warnings by best-effort DM and record the delivery result; never
  expose a target or reason publicly through allowed mentions.
- Retain moderation cases for 365 days, then purge them in bounded batches.
- Send moderation events to a configured log channel through a durable bounded
  outbox. A post failure must not undo a completed Discord action.
- Do not request `Administrator`, `Manage Roles`, Message Content, or privileged
  member intents. Request only the existing message permissions plus Moderate
  Members, Kick Members, and Ban Members. Discord and invoker role hierarchy
  restrictions cannot be bypassed.

## Legal pages and Discord verification

- Publish public, accessible SkyFeed Terms of Service and Privacy Policy pages
  through Sites, with a small policy index linking both documents.
- Describe SkyFeed as independently operated and use `jj4v3l@gmail.com` as the
  intentionally public privacy, deletion, and support contact.
- State the actual data categories: Discord guild/channel/user/role IDs,
  preferences, watch rules, moderation cases, operational logs, receiver ADS-B
  observations, and transient ADSBDB aircraft/callsign lookups.
- Explain purpose, sharing, security, retention, deletion requests, third-party
  services, and the 365-day moderation exception. The policy site must use no
  cookies, analytics, authentication, or private application data.
- Terms must cover authorized server use, staff moderation responsibility,
  acceptable use, availability, third-party services, suspension, disclaimer,
  and the rule that ADS-B data is informational and not for navigation or
  safety decisions.
- Use these public HTTPS URLs in Discord's application fields:
  - Terms of Service:
    `https://skyfeed-policies.javel-palmer.chatgpt.site/terms`
  - Privacy Policy:
    `https://skyfeed-policies.javel-palmer.chatgpt.site/privacy`
- The application Public Key is not runtime configuration for SkyFeed's
  Gateway deployment.

## Local configuration and rollout

- Keep the real `.env`, bot token, guild ID, and receiver URL outside Git.
- Store the bot token only in `secrets/discord_token` with host mode `0600`.
- Put the application ID only in the untracked local `.env`; retain placeholders
  in `.env.example`.
- Validate with synthetic receiver fixtures before connecting to the live
  receiver and development guild.
