# Claude Skills for Things

Once you've deployed the MCP server and connected it to Claude, you can create **Skills** — reusable instruction files that teach Claude how to work with your specific Things setup. Skills turn Claude from a generic assistant into one that knows your projects, tags, and workflows.

## What are Skills?

Skills are instruction files you add to Claude (Settings > Skills) that give it persistent knowledge about how you work. Without a skill, you'd need to explain your system every conversation. With a skill, Claude already knows your project structure, tag meanings, and review rituals.

## Why you need them

The MCP server gives Claude the *ability* to read and write your tasks. Skills give it the *context* to do it well. For example:

- Without a skill, Claude doesn't know which project to file a task under
- Without a skill, Claude doesn't know your tag system or what "Now" vs "Next" means to you
- Without a skill, Claude can't run a structured morning review — it'll just dump a task list

## Example skills

Here are three skill patterns that work well with this MCP server:

### 1. Daily management skill

A skill for morning reviews and day-to-day task management. Tell Claude:

- **Your areas and projects** — names and UUIDs so Claude can file tasks correctly
- **Your tag system** — what each tag means and its UUID (the MCP requires UUIDs, not names)
- **How you schedule** — do you use `when` as a soft target or a hard commitment? What's the difference between `when` and `deadline` in your workflow?
- **Morning review steps** — what you want to see each morning (today's tasks, overdue items, inbox count)
- **Overdue and deadline detection** — `things_list_today` includes overdue carry-over (Anytime tasks whose scheduled date has passed) and tasks with a deadline due today or earlier, in addition to tasks scheduled for today. No separate workaround call is needed for these. Note that `things_list_today`'s "today" boundary is the server's UTC calendar day, not your local timezone — near midnight local time this can be off by up to a day.

### 2. Weekly/monthly review skill

A skill for deeper periodic reviews:

- **Review cadence** — when you do reviews and how long they take
- **Triage rules** — when to promote (Later → Next → Now) or demote tasks
- **Stale task detection** — how old is too old for a Now-tagged task with no progress?
- **Project health checks** — which projects to review and what "stale" means for each
- **Inbox zero process** — how you want Claude to help triage inbox items

### 3. Task capture skill

A lighter skill for quick task creation throughout the day:

- **Default project mapping** — "work stuff" goes to your work project, "groceries" goes to your personal project, etc.
- **Auto-tagging rules** — what context clues map to which tags
- **Scheduling defaults** — should new tasks go to Today, Anytime, or Inbox?

## How to create a skill

### Step 1: Gather your UUIDs

Ask Claude (with the MCP connected): *"List all my projects and areas with their UUIDs"* and *"List all my tags with their UUIDs"*. Save these — you'll need them in the skill.

### Step 2: Write the skill

Create a markdown file describing your system. Structure it like this:

```markdown
---
name: my-things-daily
description: |
  Daily task management for Things 3. Handles morning reviews,
  inbox triage, and ad-hoc task work. Trigger when the user
  mentions: morning review, check tasks, add a task, reschedule.
---

# My Things Daily Management

## My System

### Projects
| Project | UUID |
|---|---|
| Work | `abc123` |
| Personal | `def456` |
| ...

### Tags
| Tag | UUID | Meaning |
|---|---|---|
| Urgent | `ghi789` | Needs attention today |
| ...

## Morning Review Steps

1. Fetch today's tasks and overdue items
2. Check inbox
3. [your steps here]

## How I Use Dates

[Explain your scheduling philosophy]

## Communication Style

[How you want Claude to talk to you during reviews]
```

### Step 3: Package and install

1. Create a folder named `my-things-daily/` containing your `SKILL.md` file
2. Zip the folder: `zip -r my-things-daily.skill my-things-daily/`
3. In Claude, go to **Settings > Skills** and upload the `.skill` file

## Tips

- **Start simple.** Begin with just your project/tag UUIDs and a basic morning review flow. Iterate after a few days of use.
- **Specify UUID format for tags.** The MCP only accepts tag UUIDs. Your skill needs to map human-readable tag names to UUIDs so Claude uses the right ones.
- **Explain your scheduling philosophy.** The difference between `when` and `deadline` varies by person. Be explicit about how you use them.
- **Add communication preferences.** Do you want brief summaries or detailed breakdowns? Should Claude suggest rewrites for vague tasks? Should it celebrate completed tasks or just move on?
