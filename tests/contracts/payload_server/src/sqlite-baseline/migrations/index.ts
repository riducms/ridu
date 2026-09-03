import * as migration_20260829_201046_sqlite_baseline from "./20260829_201046_sqlite_baseline";

export const migrations = [
	{
		up: migration_20260829_201046_sqlite_baseline.up,
		down: migration_20260829_201046_sqlite_baseline.down,
		name: "20260829_201046_sqlite_baseline",
	},
];
