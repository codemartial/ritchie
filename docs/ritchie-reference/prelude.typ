// Shared helpers for the reference chapters (each chapter imports from here).

#import "../ritchie-style.typ": theme

// Horizontal rule for a thematic break.
#let horizontalrule = {
  v(0.4em)
  align(center, line(length: 40%, stroke: 0.5pt + theme.selection))
  v(0.4em)
}

// Index mark. Place at a term's defining point; the Index appendix in main.typ
// collects these via `query(<idx>)`. `code: true` renders the term monospace.
#let idx(term, code: false) = [#metadata((term: term, code: code))<idx>]

// Heading numbering for sections: "4." for a section within its chapter.
// Chapters are numbered by each main.typ. Deeper headings stay unnumbered.
#let section-numbering(..n) = {
  let p = n.pos()
  if p.len() == 2 { str(p.at(1)) + "." }
}

// Section numbering that also numbers subsections, as "8.1" within section 8.
#let subsection-numbering(..n) = {
  let p = n.pos()
  if p.len() == 3 { str(p.at(1)) + "." + str(p.at(2)) } else { section-numbering(..p) }
}

// Numbering for a part that closes a chapter, such as its examples: the part
// heading stays unnumbered and the headings below it are numbered "1." afresh.
#let part-numbering(..n) = {
  let p = n.pos()
  if p.len() == 3 { str(p.at(2)) + "." }
}

// Chapter names used in section references, keyed by chapter label. A
// chapter without an entry is named by its heading.
#let chapter-names = (
  "chap-lexical": "Lexical",
  "chap-types": "Types",
  "chap-builtins": "Built-ins",
  "chap-temporal": "Temporal Types",
  "chap-declarations": "Declarations",
  "chap-expressions": "Expressions",
  "chap-statements": "Statements",
  "chap-errors": "Errors",
  "chap-services": "Services",
  "chap-entities": "Entities",
  "chap-queries": "Queries",
  "chap-effects": "Effects",
  "chap-stdlib": "Standard Library",
  "chap-contracts": "Contracts",
)

// Section reference to labelled section headings: "Errors section 4", or
// "section 4" within the same chapter. Several labels render as a list,
// "sections 4, 6", joined by `conj` when it is given ("sections 2 and 5"),
// or as a range from the first to the last label when `range` is true
// ("sections 4--6"). `cap` capitalizes "Section". A heading below the
// numbered depth takes the number of the section that contains it.
#let secref(..labels, conj: none, range: false, cap: false) = context {
  let targets = labels.pos().map(l => query(l).first())
  let chapter(loc) = query(heading.where(level: 1).before(loc)).last()
  let chap = chapter(targets.first().location())
  let num(h) = {
    let c = counter(heading).at(h.location())
    let s = numbering(h.numbering, ..c)
    while s == none and c.len() > 1 {
      c = c.slice(0, c.len() - 1)
      s = numbering(h.numbering, ..c)
    }
    link(h.location(), if s == none { h.body } else { s.trim(".", at: end) })
  }
  let word = if cap { "Section" } else { "section" }
  let body = if targets.len() == 1 {
    [#word #num(targets.first())]
  } else {
    let nums = targets.map(num)
    let list = if range {
      [#nums.first()--#nums.last()]
    } else if conj != none {
      nums.join(", ", last: " " + conj + " ")
    } else {
      nums.join(", ")
    }
    [#(word)s #list]
  }
  if chap.location() == chapter(here()).location() {
    body
  } else {
    let name = if chap.has("label") {
      chapter-names.at(str(chap.label), default: chap.body)
    } else {
      chap.body
    }
    if targets.len() == 1 {
      link(targets.first().location())[#name #body]
    } else {
      [#link(chap.location(), name) #body]
    }
  }
}
