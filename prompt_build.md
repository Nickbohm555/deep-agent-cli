1. Go to the current section in `@IMPLEMENTATION_PLAN.md` and focus only on that section.
2. Implement only the current section. Do not skip ahead. Read the section and follow build/test instructions from `@AGENTS.md`.
3. Before editing, inspect existing code so you do not rebuild what already exists.
4. Complete all steps in the current section. Do not skip any.
5. If the section is a testing section, update the corresponding test-plan result notes before stopping. If the source test markdown is a file such as `01-02-tests.md`, write actual outcomes back into that file and keep plan notes aligned.
6. If this section is complete, advance `Current section to work on` in `@IMPLEMENTATION_PLAN.md` by exactly one section (only if not already advanced).
7. Write `.loop-commit-msg` with exactly one non-empty line containing only the commit subject in one of these formats:
   - `{phase}-{plan}-task{task-number}`
   - `{phase}-{plan}-test{test-number}`
   - `{phase}-{plan}-summary`
   Do not include labels, bullets, markdown, or extra lines.
8. Never create commits and never run git-history-changing commands (`git commit`, `git merge`, `git rebase`, `git cherry-pick`, `git reset`, `git push`). `loop.sh` is solely responsible for committing and pushing.
9. Stop after completing that section and return 20 thumbs up so the user knows the iteration finished.

Keep `@AGENTS.md` operational only.
