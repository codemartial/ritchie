// The Ritchie Language Reference
//
// The chapters/*.typ files hold the text of the reference. The title page
// shows the month of the build, or of SOURCE_DATE_EPOCH when it is set.
//
// Usage:
//   typst compile --root .. main.typ ritchie-reference.pdf                  # light (default)
//   typst compile --root .. main.typ ritchie-reference.pdf --input theme=dark

#import "../ritchie-style.typ": *
#import "prelude.typ": section-numbering

#set document(
    title: "The Ritchie Language Reference",
    author: "Tahir Hashmi",
)

#set page(
    margin: 2cm,
    fill: theme.bg,
    footer: context {
        set text(size: 8pt, fill: theme.fg)
        let pat = here().page-numbering()
        grid(
            columns: (1fr, auto, 1fr),
            align: (left, center, right),
            [© 2026 Tahir Hashmi · CC BY-ND 4.0],
            if pat != none { numbering(pat, ..counter(page).get()) },
            [],
        )
    },
)
#set text(font: body-font, size: body-size, fill: theme.fg)
#set par(leading: 0.8em, justify: true)
#set raw(theme: theme-file, syntaxes: grammar)
#show raw.where(block: false): set text(font: code-font, size: code-size)
#show raw.where(block: true): simple-code-block

// Number chapters, and sections within each chapter. Chapters that number
// deeper headings set their own numbering (prelude.typ).
#set heading(numbering: (..n) => if n.pos().len() == 1 { numbering("1  ", ..n) } else { section-numbering(..n) })

#show: heading-styles-with-pagebreak

// ============================================================================
// TITLE PAGE
// ============================================================================

#page(numbering: none)[
  #v(1fr)
  #align(center)[
    #text(font: heading-font, size: 36pt, weight: 700)[Ritchie]
    #v(0.6em)
      #text(font: heading-font, size: 16pt, weight: 700)[The Distributed-Application Programming Language]
    #v(0.6em)
    #text(font: heading-font, size: 12pt)[Language Reference]
    #v(0.4em)
    #text(size: 10pt)[#datetime.today().display("[month repr:long] [year]")]
  ]
  #v(1fr)
  #align(center)[
    #set text(size: 8pt)
    Copyright © 2026 Tahir Hashmi. \
    Except for Ritchie code examples, this Reference is licensed under the
    #link("https://creativecommons.org/licenses/by-nd/4.0/")[Creative Commons
    Attribution-NoDerivatives 4.0 International License]. \
    Ritchie code examples are dedicated under
    #link("https://creativecommons.org/publicdomain/zero/1.0/")[CC0 1.0 Universal].
  ]
]

// ============================================================================
// OUTLINE
// ============================================================================

#page(numbering: none)[
  #text(font: heading-font, size: 1.5em, weight: 700)[Contents]
  #v(0.5em)
  #outline(title: none, indent: 1.5em, depth: 2)
]

#set page(numbering: "1")
#counter(page).update(1)

// Running header: the current chapter name, right-aligned, suppressed on each
// chapter's own opening page (where the title already shows).
#set page(header: context {
    let here-page = here().page()
    let chaps = query(heading.where(level: 1))
    let current = none
    for h in chaps {
        if h.location().page() <= here-page { current = h }
    }
    if current != none and current.location().page() != here-page {
        set text(font: heading-font, size: 8pt, fill: theme.comment)
        align(right, emph(current.body))
    }
})

// ============================================================================
// CHAPTERS
// ============================================================================

#include "chapters/lexical.typ"
#include "chapters/types.typ"
#include "chapters/builtins.typ"
#include "chapters/temporal.typ"
#include "chapters/declarations.typ"
#include "chapters/expressions.typ"
#include "chapters/precedence.typ"
#include "chapters/statements.typ"
#include "chapters/errors.typ"
#include "chapters/errors-examples.typ"
#include "chapters/services.typ"
#include "chapters/services-examples.typ"
#include "chapters/entities.typ"
#include "chapters/entities-examples.typ"
#include "chapters/queries.typ"
#include "chapters/effects.typ"
#include "chapters/effects-examples.typ"
#include "chapters/stdlib.typ"
#include "chapters/contracts.typ"

// ============================================================================
// APPENDICES
// ============================================================================

#counter(heading).update(0)
#set heading(numbering: (..n) => if n.pos().len() == 1 { "Appendix " + numbering("A", ..n) + "  " })

= Grammar <chap-grammar>

The complete grammar in extended Backus-Naur form. Lexical input and layout
are defined in the #link(<chap-lexical>)[Lexical Specification].

#raw(read("../spec/grammar.ebnf"), block: true, lang: "ebnf")

// ============================================================================
// APPENDIX B: INDEX
// ============================================================================
// Entries come from #idx(...) marks (prelude.typ) placed at each term's
// defining section in the chapters; this collects them by term, with pages.

= Index

#context {
    // term -> (code: bool, pages: unique sorted ints)
    let groups = (:)
    for h in query(<idx>) {
        let v = h.value
        let pg = counter(page).at(h.location()).first()
        let g = groups.at(v.term, default: (code: v.code, pages: ()))
        if not g.pages.contains(pg) { g.pages.push(pg) }
        groups.insert(v.term, g)
    }
    let terms = groups.keys().sorted(key: t => lower(t))
    set text(size: 9pt)
    columns(2, gutter: 1.6em)[
        #for t in terms [
            #let g = groups.at(t)
            #box(if g.code { raw(t) } else { t })#box(width: 1fr, repeat[#text(fill: theme.comment)[.]])#g.pages.sorted().map(str).join(", ") \
        ]
    ]
}
