const packageManagerNames = ["pnpm", "yarn", "bun", "npm"];
const packageManagers = new Set(packageManagerNames);

export function detectPackageManager(userAgent = "", executable = "") {
	const name = userAgent.trim().split(/[\s/]/, 1)[0]?.toLowerCase();
	if (packageManagers.has(name)) return name;
	const executableName = executable.toLowerCase().split(/[\\/]/).at(-1) ?? "";
	for (const manager of packageManagerNames) {
		if (executableName.startsWith(manager)) return manager;
	}
	return "npm";
}

export function initializerArguments(
	arguments_,
	userAgent = process.env.npm_config_user_agent ?? ""
) {
	const hasExplicitManager = arguments_.some(
		(argument) => argument === "--package-manager" || argument.startsWith("--package-manager=")
	);
	if (hasExplicitManager) return ["new", ...arguments_];
	return [
		"new",
		"--package-manager",
		detectPackageManager(userAgent, process.env.npm_execpath ?? process.execPath),
		...arguments_,
	];
}
