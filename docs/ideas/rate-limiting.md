# Rate Limiting

Right now if you hit a rate limit the agent just errors out and the task fails. That's it. No retry, no backoff, no warning that you're getting close. You find out when it breaks.

I want MENACE to handle this gracefully. If a provider says "slow down", we slow down. Ideally we never hit the wall in the first place.

The providers return rate limit info in response headers — remaining requests, reset windows, token budgets. We should be reading those and using them. No hardcoded rate limit tables. I don't want to maintain a list of limits per provider per tier that goes stale every time they change their pricing page. Read what the API tells you.

What I'm thinking:
- **Auto-retry with backoff** — if we get a 429, wait and retry instead of failing the task. The orchestrator already has retry logic, this just needs to be smarter about *when* to retry.
- **Proactive throttling** — if the headers say we're at 80% of our request budget, start spacing things out before we hit the wall. Especially important with parallel workers hammering the same API key.
- **Feedback in the UI** — show when we're being throttled. Something in the banner or a toast so the user knows why things slowed down, not just that they did.
- **Auto-resume** — if a task fails because of rate limits specifically, don't mark it failed. Park it and pick it back up when the window resets.

**Things to figure out:**
- How to share rate limit state across the architect and multiple workers hitting the same provider
- Whether to throttle at the orchestrator level (delay scheduling) or the agent level (delay requests)
- How to tell the difference between "rate limited, wait 30 seconds" and "rate limited, you're done for the day"
