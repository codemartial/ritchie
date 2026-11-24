// Ritchie Document Style
// Shared styling for all Ritchie documentation

#import "syntax-theme.typ": syntax, syntax-theme-file, ritchie-grammar

// Re-export syntax theme for convenience
#let theme = (..syntax, bg: white, fg: black)
#let theme-file = syntax-theme-file
#let grammar = ritchie-grammar

// ============================================================================
// FONTS
// ============================================================================

// Each list ends with a font that Typst embeds, so a machine without the
// preferred font still renders code in a monospace face.
#let body-font = ("Baskerville", "Libertinus Serif") //"Atkinson Hyperlegible"
#let heading-font = ("Avenir Next", "Libertinus Serif")
#let code-font = ("TX-02", "DejaVu Sans Mono")
#let body-size = 12pt
#let code-size = 9pt

// ============================================================================
// PAGE & TEXT SETUP
// ============================================================================

#let page-setup() = {
  set page(margin: 2cm, fill: syntax.bg)
  set text(font: body-font, size: body-size, fill: syntax.fg)
  set par(leading: 0.8em, justify: true)
  set raw(theme: syntax-theme-file, syntaxes: ritchie-grammar)
  show raw.where(block: false): set text(font: code-font, size: code-size)
}

// ============================================================================
// HEADINGS
// ============================================================================

// Apply with `#show: heading-styles` (NOT `#heading-styles()` -- show rules
// inside a function body don't escape when the function is called bare).
#let heading-styles(body) = {
  show heading: set text(font: heading-font)
  show heading.where(level: 1): it => {
    v(1em)
    text(size: 1.5em, weight: 700, it)
    v(0.5em)
  }
  show heading.where(level: 2): set text(size: 1.15em, weight: 600)
  show heading.where(level: 3): set text(size: 1.0em, weight: 600)
  body
}

// For design.typ which needs pagebreaks before h1.
// Apply with `#show: heading-styles-with-pagebreak`.
#let heading-styles-with-pagebreak(body) = {
  show heading: set text(font: heading-font)
  show heading.where(level: 1): it => {
    pagebreak(weak: true)
    v(1em)
    text(size: 1.5em, weight: 700, it)
    v(0.5em)
  }
  show heading.where(level: 2): set text(size: 1.15em, weight: 600)
  show heading.where(level: 3): set text(size: 1.0em, weight: 600)
  body
}

// ============================================================================
// CODE BLOCKS
// ============================================================================

// Simple code block (for raw blocks)
#let simple-code-block(it) = block(
  fill: syntax.bg,
  stroke: 1pt + syntax.selection,
  inset: 10pt,
  radius: 4pt,
  width: 100%,
)[#set text(font: code-font, size: code-size, fill: syntax.fg); #it]

// Code block with line numbers and label support
#let code-lines = state("code-lines", (:))

#let code(body, lang: "ritchie") = {
  set text(font: code-font, size: code-size, fill: syntax.fg)
  show raw: set text(font: code-font, size: code-size)
  let lines = body.text.split("\n")
  let num-width = calc.max(2, str(lines.len()).len()) * 0.7em

  // Extract labels from "// @label" comments
  let labels = (:)
  for (i, line) in lines.enumerate() {
    let match = line.match(regex("//\s*@(\w+)"))
    if match != none {
      labels.insert(match.captures.at(0), i + 1)
    }
  }
  code-lines.update(labels)

  block(
    fill: syntax.bg,
    stroke: 1pt + syntax.selection,
    inset: 0pt,
    radius: 4pt,
    width: 100%,
    stack(
      ..lines.enumerate().map(((i, line)) => {
        // Strip label comments from display
        let display-line = line.replace(regex("\s*//\s*@\w+"), "")
        box(
          width: 100%,
          inset: (x: 8pt, y: 3pt),
          fill: if calc.rem(i, 2) == 0 { syntax.bg } else { syntax.line-highlight },
          stack(
            dir: ltr,
            spacing: 12pt,
            box(width: num-width, align(right, text(fill: syntax.gutter, str(i + 1)))),
            raw(display-line, lang: lang)
          )
        )
      })
    )
  )
}

// Reference a labeled line: #L("label")
#let L(label) = context {
  let lines = code-lines.get()
  if label in lines {
    str(lines.at(label))
  } else {
    text(fill: red, "??")
  }
}

// ============================================================================
// TABLES
// ============================================================================

#let table-stroke = 0.5pt + syntax.selection
#let table-header-fill = syntax.line-highlight
