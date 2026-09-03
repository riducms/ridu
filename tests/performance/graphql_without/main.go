package main

import (
	"log"

	"github.com/riducms/ridu/tests/performance/graphqlfixture"
)

func main() {
	if err := graphqlfixture.Run(nil, 0); err != nil {
		log.Fatal(err)
	}
}
