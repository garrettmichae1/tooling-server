package chart

// chartTemplate is the only Typst source the server compiles for a chart.
// The agent sends data. The template owns the axes, the colors, and the page.
const chartTemplate = `#import "@preview/cetz:0.5.2"
#import "@preview/cetz-plot:0.1.4": plot, chart

#let brief = json("brief.json")
#set page(paper: "us-letter", flipped: true, margin: (x: 54pt, y: 48pt))
#set text(font: "Libertinus Serif", size: 11pt, fill: rgb("#1c1916"))

#let paints = (
  rgb("#1c1916"),
  rgb("#6b5344"),
  rgb("#3d5a80"),
  rgb("#8a5a44"),
)
#let paint-at(i) = paints.at(calc.rem(i, paints.len()))
#let bar-at(i) = {
  let c = paint-at(i)
  (stroke: c, fill: c.lighten(42%))
}
#let axis-label(s) = if s == "" { none } else { s }

#align(center + horizon)[
  #text(size: 22pt, weight: "bold")[#brief.title]
  #v(14pt)
  #cetz.canvas(length: 1cm, {
    let xlabel = axis-label(brief.xlabel)
    let ylabel = axis-label(brief.ylabel)
    let kind = brief.type

    if kind == "pie" {
      let data = brief.categories.zip(brief.series.at(0).values)
      chart.piechart(
        data,
        value-key: 1,
        label-key: 0,
        radius: 3.4,
        slice-style: paints,
        outer-label: (content: "%", radius: 118%),
        legend: (label: "LABEL", position: "east", anchor: "west", offset: (0.6, 0)),
      )
    } else if kind == "column" or kind == "bar" {
      let n = brief.series.len()
      let rows = range(brief.categories.len()).map(i => {
        (brief.categories.at(i),) + brief.series.map(s => s.values.at(i))
      })
      let keys = range(1, n + 1)
      let names = if n == 1 { none } else { brief.series.map(s => s.name) }
      let mode = if n == 1 {
        "basic"
      } else if brief.stacked {
        "stacked"
      } else {
        "clustered"
      }
      let value-key = if n == 1 { 1 } else { keys }
      if kind == "column" {
        chart.columnchart(
          rows,
          value-key: value-key,
          mode: mode,
          size: (13, 7.2),
          x-label: xlabel,
          y-label: ylabel,
          labels: names,
          bar-style: if n == 1 { i => bar-at(0) } else { bar-at },
        )
      } else {
        chart.barchart(
          rows,
          value-key: value-key,
          mode: mode,
          size: (13, 7.2),
          x-label: xlabel,
          y-label: ylabel,
          labels: names,
          bar-style: if n == 1 { i => bar-at(0) } else { bar-at },
        )
      }
    } else if brief.categories.len() != 0 {
      let cats = brief.categories
      plot.plot(
        size: (13, 7.2),
        axis-style: "scientific",
        x-label: xlabel,
        y-label: ylabel,
        x-grid: true,
        y-grid: true,
        x-tick-step: none,
        x-ticks: cats.enumerate(),
        x-min: -0.5,
        x-max: cats.len() - 0.5,
        y-min: if kind == "area" { 0 } else { auto },
        legend: auto,
        {
          for (i, s) in brief.series.enumerate() {
            let c = paint-at(i)
            let named = if s.name == "" { none } else { s.name }
            let pts = s.values.enumerate()
            if kind == "scatter" {
              plot.add(pts, label: named, mark: "o", mark-size: 0.16, style: (stroke: none), mark-style: (fill: c, stroke: c))
            } else if kind == "area" {
              plot.add(pts, label: named, fill: true, style: (stroke: c, fill: c.lighten(70%)))
            } else {
              plot.add(pts, label: named, mark: "o", mark-size: 0.12, style: (stroke: c), mark-style: (fill: c, stroke: c))
            }
          }
        },
      )
    } else {
      plot.plot(
        size: (13, 7.2),
        axis-style: "scientific",
        x-label: xlabel,
        y-label: ylabel,
        x-grid: true,
        y-grid: true,
        x-min: brief.xmin,
        x-max: brief.xmax,
        y-min: brief.ymin,
        y-max: brief.ymax,
        legend: auto,
        {
          for (i, s) in brief.series.enumerate() {
            let c = paint-at(i)
            let named = if s.name == "" { none } else { s.name }
            if kind == "scatter" {
              plot.add(s.points, label: named, mark: "o", mark-size: 0.16, style: (stroke: none), mark-style: (fill: c, stroke: c))
            } else if kind == "area" {
              plot.add(s.points, label: named, fill: true, style: (stroke: c, fill: c.lighten(70%)))
            } else {
              plot.add(s.points, label: named, mark: "o", mark-size: 0.12, style: (stroke: c), mark-style: (fill: c, stroke: c))
            }
          }
        },
      )
    }
  })
]
`
