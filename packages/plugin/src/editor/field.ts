/**
 * Adds a label, description and validation messages around your custom field input.
 *
 * Import `Field from "@riducms/plugin/editor/field"` and wrap your input in
 * `<Field {field}>...</Field>` to show its label, description and validation errors.
 * Your input still needs its value, read-only state and change handler. The wrapper
 * does not register the field or own form values; Ridu already does that.
 */
export { default } from "./field.svelte";
