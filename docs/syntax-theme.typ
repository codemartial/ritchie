// Ritchie Syntax Highlighting Theme
// Light: Atom One Light inspired
// Dark: Ayu Mirage inspired

#let syntax-theme = sys.inputs.at("theme", default: "light")
#let is-dark = syntax-theme == "dark"

// TextMate theme file for syntax highlighting
#let syntax-theme-file = if is-dark {
  "ayu-mirage.tmTheme"
} else {
  "atom-one-light.tmTheme"
}

// Ritchie grammar for syntax highlighting
#let ritchie-grammar = "ritchie.sublime-syntax"

// Syntax highlighting color schemes
#let syntax = if is-dark {
  // Ayu Mirage
  (
    // Base colors
    bg: rgb("#1f2430"),
    fg: rgb("#cbccc6"),

    // Syntax colors
    keyword: rgb("#ffa759"),
    string: rgb("#bae67e"),
    number: rgb("#ffcc66"),
    comment: rgb("#5c6773"),
    function: rgb("#ffd580"),
    type: rgb("#73d0ff"),
    operator: rgb("#f29e74"),
    variable: rgb("#cbccc6"),
    constant: rgb("#d4bfff"),
    property: rgb("#5ccfe6"),
    punctuation: rgb("#cbccc6"),
    tag: rgb("#5ccfe6"),
    attribute: rgb("#ffd580"),

    // Special
    error: rgb("#ff3333"),
    warning: rgb("#ffcc66"),
    info: rgb("#73d0ff"),

    // UI elements
    selection: rgb("#33415e"),
    line-highlight: rgb("#191e2a"),
    gutter: rgb("#5c6773"),
  )
} else {
  // Atom One Light
  (
    // Base colors
    bg: rgb("#fafafa"),
    fg: rgb("#383a42"),

    // Syntax colors
    keyword: rgb("#a626a4"),
    string: rgb("#50a14f"),
    number: rgb("#986801"),
    comment: rgb("#a0a1a7"),
    function: rgb("#4078f2"),
    type: rgb("#c18401"),
    operator: rgb("#383a42"),
    variable: rgb("#e45649"),
    constant: rgb("#986801"),
    property: rgb("#4078f2"),
    punctuation: rgb("#383a42"),
    tag: rgb("#e45649"),
    attribute: rgb("#986801"),

    // Special
    error: rgb("#e45649"),
    warning: rgb("#c18401"),
    info: rgb("#4078f2"),

    // UI elements
    selection: rgb("#e5e5e6"),
    line-highlight: rgb("#f0f0f1"),
    gutter: rgb("#9d9d9f"),
  )
}

// Helper function to apply syntax highlighting to inline code
#let hl(content, kind) = {
  let color = syntax.at(kind, default: syntax.fg)
  text(fill: color, content)
}

// Styled code block with syntax theme
#let code-block(body, lang: none) = {
  block(
    fill: syntax.bg,
    stroke: 1pt + syntax.selection,
    radius: 6pt,
    inset: 16pt,
    width: 100%,
    breakable: false,
  )[
    #set text(font: "Berkeley Mono Variable", size: 11pt, fill: syntax.fg)
    #if lang != none {
      place(
        top + right,
        dx: -8pt,
        dy: -8pt,
        text(size: 8pt, fill: syntax.comment, lang)
      )
    }
    #body
  ]
}

// Inline code with syntax theme
#let inline-code(body) = {
  box(
    fill: syntax.bg,
    stroke: 0.5pt + syntax.selection,
    radius: 3pt,
    inset: (x: 4pt, y: 2pt),
  )[
    #set text(font: "Berkeley Mono Variable", size: 0.9em, fill: syntax.fg)
    #body
  ]
}

// Code block with line numbers
#let numbered-code(body, lang: none, start: 1) = {
  let lines = if type(body) == content and body.has("text") {
    body.text.split("\n")
  } else if type(body) == str {
    body.split("\n")
  } else {
    (repr(body),)
  }

  block(
    fill: syntax.bg,
    stroke: 1pt + syntax.selection,
    radius: 6pt,
    inset: 0pt,
    width: 100%,
    breakable: false,
    clip: true,
  )[
    #set text(font: "Berkeley Mono Variable", size: 11pt, fill: syntax.fg)
    #if lang != none {
      place(
        top + right,
        dx: -12pt,
        dy: 4pt,
        text(size: 8pt, fill: syntax.comment, lang)
      )
    }
    #table(
      columns: (auto, 1fr),
      inset: (x: 12pt, y: 6pt),
      stroke: none,
      column-gutter: 0pt,
      ..lines.enumerate().map(((i, line)) => {
        let line-num = i + start
        let bg = if calc.rem(i, 2) == 0 { syntax.line-highlight } else { syntax.bg }
        (
          table.cell(fill: bg)[
            #text(fill: syntax.gutter, str(line-num))
          ],
          table.cell(fill: bg)[#line],
        )
      }).flatten()
    )
  ]
}
