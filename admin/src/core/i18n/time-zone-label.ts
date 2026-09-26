/** Describe a configured timezone using its offset and localized name at the displayed instant. */
export function timeZoneLabel(id: string, label: string, language: string, at: Date) {
	const fixedOffset = /^[+-]\d{2}:\d{2}$/.test(id);
	const offset = new Intl.DateTimeFormat("en", {
		timeZone: id,
		timeZoneName: "longOffset",
	})
		.formatToParts(at)
		.find((part) => part.type === "timeZoneName")!.value;
	const name = new Intl.DateTimeFormat(language, {
		timeZone: id,
		timeZoneName: "long",
	})
		.formatToParts(at)
		.find((part) => part.type === "timeZoneName")!.value;
	const utcOffset = offset === "GMT" ? "UTC+00:00" : offset.replace("GMT", "UTC");
	const description =
		fixedOffset || name === label || name === offset ? label : `${label} (${name})`;
	return `(${utcOffset}) ${description}`;
}
