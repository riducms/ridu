---
title: 'Go packages'
description: 'Understand the Go packages and value types used in Ridu examples, and choose the right one for your code.'
product: core
eyebrow: 'Go packages'
order: 37
aliases:
  [
    'Go API essentials',
    'Go imports',
    'Go package overview',
    'Ridu types'
  ]
navigation:
  section: 'Get started'
  order: 55
  title: 'Go packages'
---

In `store.String("Hello")`, `store` is the name of a Go package and `String` is a function in
that package. Ridu groups its Go API by the work you are doing: defining a field, changing its
value, reading a document, or building a filter.

You will see several packages in one example because those jobs often meet in the same function.
These guides explain the types together, with examples you can read without opening the API
reference. If the Go syntax itself is unfamiliar, start with
[Go for TypeScript developers](/docs/go-for-typescript/).

## Choose the package for your task {#choose-a-package}

| When you need to…                                                    | Use         | Start here                                               |
| -------------------------------------------------------------------- | ----------- | -------------------------------------------------------- |
| Define an application, collection, global, or document hook          | `ridu`      | [Configuration](/docs/configuration/)                    |
| Define a text field, relationship, group, or field rule              | `field`     | [Fields](/docs/fields/)                                  |
| Read a field callback's values or return a replacement               | `operation` | [Operations and callbacks](/docs/go-packages/operation/) |
| Build document data or read values returned by the local API         | `store`     | [Documents and values](/docs/go-packages/store/)         |
| Describe which documents a query or access rule should match         | `query`     | [Filters and paths](/docs/go-packages/query/)            |
| Understand resolved field definitions, resource IDs, or locale types | `schema`    | [Schema and identifiers](/docs/go-packages/schema/)      |

The import paths start with `github.com/riducms/ridu`. For example, import
`"github.com/riducms/ridu/operation"` to use `operation.Keep(...)`. The root import,
`"github.com/riducms/ridu"`, provides `ridu.Collection`, `ridu.Config`, and `ridu.HookContext`.

### Does using `store` mean writing database code? {#store-name}

No. The local Go API uses `store.Values` for document data, so ordinary application code may
import `store` just to write a title or read a number. You still call `app.Local()` or a generated
collection handle to save a document. Ridu runs permissions, validation, and hooks for that call.

The package also contains the database adapter interface. You only implement that interface when
building an adapter. Upload file bytes use the separate `storage` package; see
[File storage](/docs/storage/).

## Three types named Value {#value-types}

These types answer different questions:

| Type                 | Question it answers                          | Example                                                  |
| -------------------- | -------------------------------------------- | -------------------------------------------------------- |
| `store.Value`        | What kind of document data is this?          | `store.String("Hello")` is a string value                |
| `operation.Value[T]` | Does this callback have a value of type `T`? | `operation.Present("Hello")` carries a present Go string |
| `query.Value`        | What should this filter compare against?     | `query.String("Hello")` is the filter's comparison value |

In `operation.Value[string]`, the square brackets mean the callback works with a Go `string`.
`operation.Value[store.Value]` carries an optional document value, which can hold an object,
list, or other supported data. Raw hooks use it before type checks; validators for groups,
arrays, blocks, and JSON also use it after checks. The callback's phase tells you what has
already been validated.

The wrappers do not save or query anything by themselves. A field transform returns an
`operation.Change[T]`; a local API call receives `store.Values`; a filter combines `query.Value`
with a path and an operator.

Follow [callback values](/docs/go-packages/operation/) for missing values and `Keep`/`Replace`,
[document values](/docs/go-packages/store/) for constructors and accessors, or
[query values](/docs/go-packages/query/) for comparisons. You do not need to convert between all
three in every function.

## What does ctx contain? {#context}

`ctx` is a variable name, not one particular Ridu type. Check the function argument:

| Argument type                                                                                    | What you have                                                                                |
| ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| `context.Context`                                                                                | Go request cancellation and deadlines; pass it to work started for the request               |
| `ridu.AccessContext`                                                                             | A collection or global access check, including the caller and requested operation            |
| `ridu.HookContext`                                                                               | A document hook, including editable input, the original document, and the local API          |
| `operation.DefaultContext`                                                                       | A field default, including the caller, content locale, and input available before validation |
| `operation.LiveValidationContext`                                                                | A check while editing, including unsaved input, saved values, and the caller                 |
| `operation.ValidationContext`, `WriteContext`, `ReadContext`, `AccessContext`, or `EventContext` | A field callback, including nearby values, previous values, and the caller                   |

The distinction matters. A document hook's `ctx.Actor` can be `nil`; a field callback's
`ctx.Actor.ID` is empty for an anonymous caller. A document hook can change entries in `ctx.Data`
before saving. Field contexts provide read-only snapshots; a field transform returns a
replacement to change its own value.

Read [field callback contexts](/docs/go-packages/operation/) and
[document hook context](/docs/hooks/context/) for examples. The name of the argument alone does
not tell you which properties are available.

## Choose what to return {#return-values}

The callback decides the return type; a function name such as `validateTitle` does not.

| Callback                                | Usual result                                      | Other result                                       |
| --------------------------------------- | ------------------------------------------------- | -------------------------------------------------- |
| Collection or global access rule        | `ridu.Allow(), nil`                               | `ridu.Deny(), nil` or `ridu.Where(filter), nil`    |
| Field access rule                       | `true, nil`                                       | `false, nil`                                       |
| Field validator, including live checks  | `nil, nil`                                        | A list of `operation.Issue` messages and `nil`     |
| Field default                           | `operation.Present(value), nil` to supply a value | `operation.Empty[T](), nil` to supply no default   |
| Field transform                         | `operation.Keep[T](), nil`                        | `operation.Replace(operation.Present(value)), nil` |
| Field event hook, such as `AfterChange` | `nil`                                             | An `error` if the work failed                      |
| Collection or global hook               | `nil`                                             | An `error` if the work failed                      |

The final `nil` in a two-result return means “no execution error.” A denied permission or a
validation message is an expected result. An `error` means the function could not finish its
work, for example because a related lookup failed. Return that error instead of silently allowing
the request.

[Access control](/docs/access-control/), [Field defaults](/docs/fields/defaults/), [Validation](/docs/fields/validation/),
[Live server validation](/docs/fields/live-validation/), and
[Hooks](/docs/hooks/) show where each callback is registered. Use the guides in this section when
you need to understand the values inside those callbacks.
