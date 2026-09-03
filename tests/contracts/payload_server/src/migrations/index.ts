import * as migration_20260815_053403_performance_baseline from "./20260815_053403_performance_baseline";
import * as migration_20260828_215931_performance_fixture_sync from "./20260828_215931_performance_fixture_sync";

export const migrations = [
	{
		up: migration_20260815_053403_performance_baseline.up,
		down: migration_20260815_053403_performance_baseline.down,
		name: "20260815_053403_performance_baseline",
	},
	{
		up: migration_20260828_215931_performance_fixture_sync.up,
		down: migration_20260828_215931_performance_fixture_sync.down,
		name: "20260828_215931_performance_fixture_sync",
	},
];
