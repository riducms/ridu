---
title: 'Date field'
description: 'Add a date picker for a calendar date, time, or date and time together.'
product: core
eyebrow: 'Basic fields'
order: 65
aliases:
  ['field.Date', 'date time field', 'datetime picker', 'DateFormat']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Date',
    'go:github.com/riducms/ridu/field#DateFormat'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 50
  title: 'Date'
---

Use `field.Date` for a calendar date, a time of day, or a date and time together. The API accepts
and returns a string in the format you choose.

## In the admin {#admin-behavior}

![A populated Publication date field beside its open September 2026 calendar in the Ridu admin.](../../../../../docs/assets/fields/date.png)

_A DateTime field stores an RFC 3339 timestamp; the admin selects the matching date-and-time control._

## Choose a date or time format {#example}

```go title="content/events.go"
field.Date("startsAt").
	Required().
	Format(field.DateTime)
```

| Format     | Accepted value        | Use it for                                        |
| ---------- | --------------------- | ------------------------------------------------- |
| `DateOnly` | `YYYY-MM-DD`          | Birthdays, release days, and other calendar dates |
| `DateTime` | RFC 3339 timestamp    | An instant such as `2027-06-10T09:30:00Z`         |
| `TimeOnly` | `HH:mm` or `HH:mm:ss` | A local opening time without a date               |

The field format controls server validation, OpenAPI, and the matching admin control. Omitting
`Format` uses `field.DateOnly`. Changing its `Admin` settings does not change the accepted format.

## Configuration {#configuration}

| Constructor or method                                 | What it controls                                                 |
| ----------------------------------------------------- | ---------------------------------------------------------------- |
| `field.Date(name)`                                    | Creates a stored date string.                                    |
| `.Format(field.DateOnly)` / `.Format(field.DateTime)` | Accepts a calendar date or an RFC 3339 timestamp.                |
| `.Required()`                                         | Rejects a missing, null, or empty date.                          |
| `.Default(value)` / `.DefaultFrom(callback)`          | Supplies a fixed or request-aware initial date string.           |
| `.Index()` / `.Unique()`                              | Adds an index for filtering/sorting or enforces distinct values. |
| `.Localized()`                                        | Stores a separate date value for each configured content locale. |
| `.Validate(callback)` / `.LiveValidate(callback)`     | Adds domain-specific save or live checks.                        |

## Defaults, timezones, and sorting {#options}

Use `.Default(...)` for a fixed date string or `.DefaultFrom(callback)` to calculate an initial
date when saving, such as the current time. A Date callback returns `operation.Value[string]`
matching the field's `Format`. For a DateTime field, `.Default(time.Now().Format(time.RFC3339))`
fixes the timestamp when your configuration is built. Calculate it inside `.DefaultFrom(...)`
when it should change with each new document. See [Set default field values](/docs/fields/defaults/).

Date also supports `Required`, localization, uniqueness/indexing, and common admin options.
Filters and sorts compare normalized accepted date strings. Use RFC 3339 with an explicit
offset for day-and-time values; convert for display in the consumer rather than removing timezone
information before storage.

A localized date is appropriate only when the underlying content really differs by locale. A
single event instant usually should not be localized merely because each audience formats it
differently.

## Common mistakes {#troubleshooting}

- Do not send a JavaScript `Date` object directly. Serialize a day/time string matching the chosen
  format.
- A day-only value has no timezone. Do not convert it through midnight UTC and accidentally move it
  to another day.
- Time-only values need an application-owned timezone and recurrence model if they represent store
  hours or schedules.

See [`field.Date`](/reference/field/date/),
[`DateField.Format`](/reference/field/date-field-format-method/), and [querying](/docs/querying/).
