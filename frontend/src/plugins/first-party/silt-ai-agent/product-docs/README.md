# Product help corpus (agent)

Curated, user-facing help articles shipped with the AI agent. The
`search_product_docs` tool ranks sections from these files at runtime.

## Adding or updating an article

1. Add or edit a `*.md` file in this folder (not this README).
2. Start with YAML frontmatter:

   ```markdown
   ---
   id: short-stable-id
   title: Human-readable title
   ---
   ```

3. Use `##` headings for searchable sections. Keep articles short and
   task-oriented; match current UI labels (Settings → AI, Silt AI drawer, etc.).
4. Prefer plain steps over engineering detail. Do **not** paste SPECS,
   ARCHITECTURE, or ADRs into the default corpus.
5. When product docs under `docs/` change, update the curated excerpt here
   (do not auto-import eng docs wholesale).
6. Run agent unit tests (`search_product_docs` / corpus search) after edits.
7. Prefer covering real user how-tos (slash commands, tables, toolbar paths)
   with current UI labels from the app — not engineering SPECS dumps.
