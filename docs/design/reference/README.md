# V4 design references

The two images here are the human-approved V4 Modern Everyday design references. Both are AI visual concepts. Neither is a screenshot of Tendo, so do not use them as evidence of an implemented feature, and do not use them in the README or release notes.

| File | Size | SHA-256 |
| --- | --- | --- |
| `tendo-v4-desktop.png` | 1448×1086 | `9fb1901bc30d35a3df8c8a434b17edc1ba9708dc9260f36befdb209dc746a61d` |
| `tendo-v4-mobile.png` | 941×1672 (about a 390 CSS px viewport) | `40a1571f1fb88e18f3ea2f84d60ed1967cba73fd59c3c8d3cb9d3c7f98520198` |

V4-00 (#408) copied both images byte-for-byte from the approved bootstrap packet. Before copying, it checked them against the hashes in the header of [`.autonomous/V4_DESIGN_BRIEF.md`](../../../.autonomous/V4_DESIGN_BRIEF.md), which is the binding source. Verify them with:

```bash
sha256sum docs/design/reference/tendo-v4-*.png
```

Do not edit or replace them. The brief's "Binding reference fidelity contract" says how real production captures must be compared with them.
