package migrationartifact

import "fmt"

// AdoptionEnd decides how much of a pending history a baseline may record as
// applied without running it, on a database that schema sync already brought
// forward. It returns the history length after adoption: the newest artifact
// whose resulting schema the database has, provided no artifact up to it has a
// step schema sync never runs. Later artifacts stay pending for ridu migrate up.
//
// matches reports whether the database's physical schema equals the schema
// after files[index]; blockingStep names a step that only a migration runner
// performs, or returns "" when every step is physical.
func AdoptionEnd(files []File, applied int, blockingStep func(File) string, matches func(index int) (bool, error)) (int, error) {
	blocked := ""
candidates:
	for end := len(files); end > applied; end-- {
		matched, err := matches(end - 1)
		if err != nil {
			return 0, err
		}
		if !matched {
			continue
		}
		for _, file := range files[applied:end] {
			if kind := blockingStep(file); kind != "" {
				if blocked == "" {
					blocked = fmt.Sprintf("migration %s has a %s step, which schema sync never runs, so it cannot be adopted; recreate this database with ridu migrate up, or apply that migration on a database that migrations manage", file.Name, kind)
				}
				continue candidates
			}
		}
		return end, nil
	}
	if blocked != "" {
		return 0, fmt.Errorf("%s", blocked)
	}
	if applied > 0 {
		matched, err := matches(applied - 1)
		if err != nil {
			return 0, err
		}
		if matched {
			return applied, nil
		}
	}
	return 0, fmt.Errorf("the database schema matches no committed migration, so it cannot be adopted; create a migration for the current config with ridu migrate create, or recreate the database with ridu migrate up")
}
