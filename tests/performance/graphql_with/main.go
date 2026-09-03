package main

import (
	"log"
	"os"
	"strconv"

	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/tests/performance/graphqlfixture"
)

func main() {
	requests, _ := strconv.Atoi(os.Getenv("RIDU_GRAPHQL_REQUESTS"))
	if err := graphqlfixture.Run(graphqlplugin.New(), requests); err != nil {
		log.Fatal(err)
	}
}
