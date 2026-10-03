/** CatalogEntry is a skill Berth offers to add in one click. */
export interface CatalogEntry {
  id: string;
  description: string;
  repo: string;
  path: string;
  ref: string;
}

const ANTHROPIC = "https://github.com/anthropics/skills";

const entry = (id: string, description: string): CatalogEntry => ({
  id,
  description,
  repo: ANTHROPIC,
  path: `skills/${id}`,
  ref: "main",
});

/** CATALOG is a short list from anthropics/skills; adding one resolves main. */
export const CATALOG: CatalogEntry[] = [
  entry("pdf", "Read, fill and create PDF files"),
  entry("docx", "Create and edit Word documents"),
  entry("xlsx", "Spreadsheets with formulas and charts"),
  entry("pptx", "Create and edit slide decks"),
  entry("mcp-builder", "Guide for building MCP servers"),
  entry("skill-creator", "Write and refine new skills"),
  entry("webapp-testing", "Test local web apps with Playwright"),
  entry("frontend-design", "Distinctive, production-grade frontend interfaces"),
  entry("brand-guidelines", "Apply brand colors and typography"),
  entry("canvas-design", "Posters and visual art as PNG or PDF"),
];
