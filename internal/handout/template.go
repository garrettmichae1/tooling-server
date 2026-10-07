package handout

// handoutTemplate is the only Typst source the server compiles.
// User text arrives through brief.json and is inserted as data.
const handoutTemplate = `#import "@preview/mitex:0.2.7": mi, mitex
#import "@preview/whalogen:0.3.0": ce
#import "@preview/cetz:0.5.2"
#let brief = json("brief.json")
#set page(paper: "us-letter", margin: (x: 54pt, y: 56pt), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt, fill: rgb("#1c1916"))
#show raw: set text(font: "JetBrains Mono", size: 9pt)
#let show-parts(items) = {
  items.map(part => {
    let c = part.at("chem", default: "")
    let m = part.at("math", default: "")
    if c != "" { ce(c) } else if m != "" { mi(m) } else { part.at("text", default: "") }
  }).join()
}
#if brief.at("kicker", default: "") != "" [
  #text(fill: rgb("#6b5344"), size: 9pt, weight: "bold", tracking: 0.08em)[#upper(brief.kicker)]
  #v(8pt)
]
#text(size: 22pt, weight: "bold")[#brief.title]
#if brief.at("subtitle", default: "") != "" [
  #v(4pt)
  #text(fill: rgb("#5c564e"), size: 12pt)[#brief.subtitle]
]
#v(14pt)
#for section in brief.sections [
  #if section.at("heading", default: "") != "" [
    #v(8pt)
    #text(size: 14pt, weight: "bold", fill: rgb("#6b5344"))[#section.heading]
    #v(6pt)
  ]
  #for node in section.body [
    #if node.type == "paragraph" [
      #par(show-parts(node.parts))
      #v(6pt)
    ] else if node.type == "math" [
      #align(center, mitex(node.latex))
      #v(6pt)
    ] else if node.type == "list" [
      #if node.at("ordered", default: false) [
        #enum(..node.items.map(show-parts))
      ] else [
        #list(..node.items.map(show-parts))
      ]
      #v(6pt)
    ] else if node.type == "code" [
      #std.block(fill: rgb("#f4f0e6"), inset: 10pt, radius: 4pt, width: 100%)[
        #raw(node.text, lang: node.lang, block: true)
      ]
      #v(6pt)
    ] else if node.type == "terms" [
      #for term in node.items [
        #strong[#term.name]
        #h(8pt)
        #show-parts(term.parts)
        #linebreak()
      ]
      #v(4pt)
    ] else if node.type == "note" [
      #std.block(fill: rgb("#f4f0e6"), inset: 12pt, radius: 4pt, width: 100%)[
        #text(fill: rgb("#6b5344"), weight: "bold", size: 9pt)[#upper(node.label)]
        #v(4pt)
        #show-parts(node.parts)
      ]
      #v(6pt)
    ] else if node.type == "reaction" [
      #align(center, ce(node.formula))
      #v(8pt)
    ] else if node.type == "figure" [
      #cetz.canvas(length: 1cm, {
        import cetz.draw: line, circle, rect, content
        for shape in node.shapes {
          if shape.kind == "line" {
            line(shape.from, shape.to)
          } else if shape.kind == "circle" {
            circle(shape.at, radius: shape.radius)
          } else if shape.kind == "rect" {
            rect(shape.at, (shape.at.at(0) + shape.width, shape.at.at(1) + shape.height))
          } else if shape.kind == "polygon" {
            line(..shape.points, close: true)
          } else if shape.kind == "label" {
            content(shape.at, [#shape.text])
          }
        }
      })
      #v(8pt)
    ]
  ]
]
`
