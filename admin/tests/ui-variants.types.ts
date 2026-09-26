import { cv, type VariantProps } from "@riducms/ui";

const styles = cv({
	base: "example",
	variants: {
		tone: { neutral: "example--neutral", warning: "example--warning" },
		size: { small: "example--small", large: "example--large" },
	},
	defaultVariants: { tone: "neutral" },
});

const selection: VariantProps<typeof styles> = { tone: "warning", size: undefined };
const className: string = styles({ ...selection, class: "app-example" });
void className;

// @ts-expect-error Variant choices are inferred from the configuration.
styles({ tone: "danger" });
// @ts-expect-error Unknown axes are not component props.
styles({ density: "compact" });
// @ts-expect-error Class arrays belong in Svelte's class attribute.
styles({ class: ["app-example", { selected: true }] });

cv({
	base: "example",
	variants: { tone: { neutral: "example--neutral" } },
	// @ts-expect-error Defaults cannot introduce choices absent from the variant map.
	defaultVariants: { tone: "warning" },
});

cv({
	base: "example",
	// @ts-expect-error The class option is reserved for additional classes.
	variants: { class: { custom: "example--custom" } },
});
