// Tests for the docs sync (node --test). Each case is something the old Jekyll site did that must keep working.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { transform, slugOf, splitFront } from './sync.mjs';

test('slugs are lower case, and README or index is the folder', () => {
  assert.equal(slugOf('guides/console-tour.md'), 'guides/console-tour');
  assert.equal(slugOf('ROADMAP.md'), 'roadmap');
  assert.equal(slugOf('console/README.md'), 'console');
  assert.equal(slugOf('api/index.md'), 'api');
});

test('the title comes from front matter, and a matching first heading is removed', () => {
  const out = transform('guides/x.md', '---\ntitle: Console tour\nparent: Guides\nnav_order: 1\n---\n\n# Console tour\n\nBody.\n');
  const { front, body } = splitFront(out);
  assert.equal(front.title, 'Console tour');
  assert.equal(front.parent, undefined, 'Jekyll-only keys are dropped');
  assert.ok(!body.includes('# Console tour'));
  assert.ok(body.startsWith('Body.'));
});

test('a page without front matter takes its first heading as the title', () => {
  const { front, body } = splitFront(transform('design/035-blueprints.md', '# 035: Blueprints and the design canvas\n\nStatus: built.\n'));
  assert.equal(front.title, '035: Blueprints and the design canvas');
  assert.ok(body.startsWith('Status: built.'));
});

test("Jekyll's relative_url becomes a path under the site's base", () => {
  const out = transform('guides/x.md', "# X\n\n![Map]({{ '/console/89-watch-map.png' | relative_url }}) and [tour]({{ '/guides/console-tour/' | relative_url }}).\n");
  assert.ok(out.includes('](/forgeline/console/89-watch-map.png)'));
  assert.ok(out.includes('](/forgeline/guides/console-tour/)'));
});

test('kramdown callouts become asides', () => {
  const out = transform('x.md', '# X\n\n{: .warning }\nPre-alpha. Be careful.\n\nNext.\n');
  assert.ok(out.includes(':::caution\nPre-alpha. Be careful.\n:::'));
  assert.ok(out.includes('Next.'));
});

test('links to other pages become site links; links out of docs go to GitHub', () => {
  const out = transform('design/037-missions.md', '# M\n\n[note 014](014-agentic-missions.md), [roadmap](../ROADMAP.md#next), [plan](../PLAN.md), [readme](../../README.md), [web](https://example.com/a.md), [here](#top).\n');
  assert.ok(out.includes('](/forgeline/design/014-agentic-missions/)'));
  assert.ok(out.includes('](/forgeline/roadmap/#next)'));
  assert.ok(out.includes('](https://github.com/forgelinehq/forgeline/blob/main/docs/PLAN.md)'), 'PLAN is not on the site');
  assert.ok(out.includes('](https://github.com/forgelinehq/forgeline/blob/main/README.md)'));
  assert.ok(out.includes('](https://example.com/a.md)'));
  assert.ok(out.includes('](#top)'));
});

test('every page links back to its source for editing', () => {
  assert.ok(transform('guides/x.md', '# X\n').includes('editUrl: "https://github.com/forgelinehq/forgeline/edit/main/docs/guides/x.md"'));
});

test('the Markdown copy has the title as a heading, asides as quotes, and full addresses', async () => {
  const { markdownCopy } = await import('./sync.mjs');
  const page = transform('guides/x.md', "---\ntitle: Tour\n---\n\n{: .note }\nRead this.\n\nSee [map]({{ '/console/m.png' | relative_url }}).\n");
  const copy = markdownCopy(page);
  assert.ok(copy.startsWith('# Tour\n\n'));
  assert.ok(copy.includes('> **Note:** Read this.'));
  assert.ok(copy.includes('](https://forgelinehq.github.io/forgeline/console/m.png)'));
  assert.ok(!copy.includes('editUrl'));
});

test('llms.txt lists every page by section, with links to the Markdown copies', async () => {
  const { llmsTxt } = await import('./sync.mjs');
  const out = llmsTxt([{ slug: 'design/037-missions', title: '037: Missions' }, { slug: 'guides/console-tour', title: 'Console tour' }, { slug: 'get-started', title: 'Get started' }, { slug: 'roadmap', title: 'Roadmap' }]);
  assert.ok(out.startsWith('# Forgeline\n\n> '));
  const at = (s) => out.indexOf(s);
  assert.ok(at('## Start here') < at('## Guides') && at('## Guides') < at('## Project') && at('## Project') < at('## Design notes'));
  assert.ok(out.includes('- [Console tour](https://forgelinehq.github.io/forgeline/guides/console-tour.md)'));
  assert.ok(out.includes('- [Roadmap](https://forgelinehq.github.io/forgeline/roadmap.md)'));
});
