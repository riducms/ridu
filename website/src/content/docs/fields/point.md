---
title: 'Point field'
description: 'Store a portable longitude/latitude coordinate pair with bounded validation.'
product: core
eyebrow: 'Structured fields'
order: 72
aliases: ['field.Point', 'geolocation field', 'longitude latitude']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Point']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Structured'
  order: 120
  title: 'Point'
---

Use `field.Point` for one geographic coordinate. The portable value is a two-number array in
**longitude, latitude** order: `[longitude, latitude]`.

## In the admin {#admin-behavior}

![A populated Location point field in the Ridu admin with longitude and latitude controls.](../../../../../docs/assets/fields/point.png)

_Separate inputs write a `[longitude, latitude]` tuple; coordinate order is part of the API._

## Smallest working example {#example}

```go title="content/venues.go"
field.Point(
	"location",
	field.Required(),
	field.Description("Drag the marker to the venue entrance."),
)
```

```json title="document.json"
{
	"location": [-0.1276, 51.5072]
}
```

Longitude must be finite and between `-180` and `180`; latitude must be finite and between `-90`
and `90`. Ridu rejects an array with the wrong length, reversed object-shaped coordinates, numeric
strings, `NaN`, or infinities.

## Options and queries {#options}

Point supports `Required`, `Localized`, conditions, descriptions, labels, layout options, and a
paired custom admin component. It does not accept numeric `Min`, `Max`, or `Step`: coordinate bounds
are part of the field contract.

Select the point as one property. The Ridu query API does not expose raw PostGIS or MongoDB driver
handles. Add an indexed search integration for radius, polygon, or route queries.

Localization stores a separate coordinate pair per locale. This is unusual but useful when the
content itself represents locale-specific offices; presentation formatting alone does not require
localization.

## Common mistakes {#troubleshooting}

- The order is longitude first. `[51.5072, -0.1276]` points somewhere else.
- A Point is not a postal address, accuracy reading, or geometry collection. Model those as sibling
  fields or a Group.
- Validate whether `0,0` is meaningful to your application; it is geographically valid and Ridu
  will not treat it as missing.

See [`field.Point`](/reference/field/point/) and [custom fields](/guides/custom-fields/).
