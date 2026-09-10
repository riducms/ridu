module example.com/ridu-gate1

go 1.25.13

require github.com/riducms/ridu v0.0.0

require (
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)

// Repository-local compile probe only; never a published application dependency.
replace github.com/riducms/ridu => ../../..
