# Forgeline documentation site

[Starlight](https://starlight.astro.build) (Astro, MIT), published to https://forgelinehq.github.io/forgeline/ by
`.github/workflows/pages.yml` on every push to `main` that touches `docs/` or `site/`.

Pages are written in `../docs` as plain Markdown, so they also read well on GitHub. `scripts/sync.mjs` copies them into
`src/content/docs` before each build: it takes the title from front matter or the first `# ` heading, turns Jekyll's
`relative_url` paths and kramdown callouts into their Starlight forms, points `.md` links at the pages, and copies the
screenshots in `docs/console`. The generated folders are not committed.

```sh
npm ci
npm run dev      # sync, then a live preview on http://localhost:4321/forgeline/
npm run build    # sync, then the static site in dist/
npm test         # the sync's tests (also run in CI)
```

- The home page (hero and cards) is `src/home.mdx`.
- The sidebar is in `astro.config.mjs`: add a new guide there; design notes are listed on their own.
- Colours and fonts match the console (`src/styles/custom.css`, fonts in `public/fonts`, OFL).
