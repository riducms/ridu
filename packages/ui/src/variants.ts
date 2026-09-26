type VariantMap = Record<string, Record<string, string>>;
type VariantSelection<T extends VariantMap> = {
	[K in keyof T]?: (keyof T[K] & string) | undefined;
};

export type VariantProps<T extends (props: never) => unknown> = Omit<
	NonNullable<Parameters<T>[0]>,
	"class"
>;

/** Select semantic classes without utility merging; CSS owns their precedence. */
export function cv<const T extends VariantMap>({
	base,
	variants,
	defaultVariants = {},
}: {
	base: string;
	variants: T & { class?: never };
	defaultVariants?: NoInfer<VariantSelection<T>>;
}): (props?: VariantSelection<T> & { class?: string | undefined }) => string {
	const keys = Object.keys(variants) as (keyof T & string)[];
	return (props = {}) => {
		const classes = [base];
		for (const key of keys) {
			const selection = props[key] ?? defaultVariants[key];
			const className = selection === undefined ? undefined : variants[key]?.[selection];
			if (className) classes.push(className);
		}
		if (props.class) classes.push(props.class);
		return classes.join(" ");
	};
}
