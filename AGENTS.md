# AGENTS

<skills_system priority="1">

## Available Skills

<!-- SKILLS_TABLE_START -->
<usage>
When users ask you to perform tasks, check if any of the available skills below can help complete the task more effectively. Skills provide specialized capabilities and domain knowledge.

How to use skills:
- Each skill has a <path> tag containing the full path to the SKILL.md file
- Read the skill file directly using: Read("<path-from-skill-tag>")
- The skill content will load with detailed instructions on how to complete the task
- The parent directory of SKILL.md contains bundled resources (references/, scripts/, assets/)

Usage notes:
- Only use skills listed in <available_skills> below
- Do not invoke a skill that is already loaded in your context
- Each skill invocation is stateless

Skills directory: /Users/samson/.claude/skills
</usage>

<available_skills>

</available_skills>
<skill>
<name>daily-summary</name>
<description>Generate daily work reports from Cursor chat history. Use when users request work summaries, daily reports, or need to review daily work content. Supports querying sessions by date, reading conversations, identifying work types, extracting tech debt, and saving summaries. Can optionally integrate git commit analysis.</description>
<path>/Users/samson/.claude/skills/daily-summary/SKILL.md</path>
<location>global</location>
</skill>

<skill>
<name>go-react-stack</name>
<description>Create a new Go+React full-stack project with DDD architecture, OpenSpec specifications, and React best practices. Includes complete project scaffolding with backend (Go with Gin), frontend (React with TypeScript), OpenSpec structure, and example code following project conventions. Use this skill when users request creating a new full-stack project, scaffolding a Go+React application, or setting up a project with OpenSpec specifications.</description>
<path>/Users/samson/.claude/skills/go-react-stack/SKILL.md</path>
<location>global</location>
</skill>

<skill>
<name>openspec</name>
<description>Specification-driven development workflow tool for reaching consensus on requirements before coding. Use when working with OpenSpec workflows, creating change proposals, implementing approved changes, or archiving completed work.</description>
<path>/Users/samson/.claude/skills/openspec/SKILL.md</path>
<location>global</location>
</skill>

<!-- SKILLS_TABLE_END -->

</skills_system>
