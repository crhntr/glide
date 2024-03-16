package glide_test

import (
	"context"
	"fmt"
	"log"

	"github.com/crhntr/glide"
)

func Example() {
	concourse := glide.Client{}
	ctx := context.Background()
	const teamName = "main"
	teams, err := concourse.Teams(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, team := range teams {
		pipelines, err := concourse.Pipelines(ctx, team.Name)
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range pipelines {
			jobs, err := concourse.Jobs(ctx, teamName, p.Name)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%#v\n", p)
			for _, j := range jobs {
				fmt.Printf("\t%#v\n", j)
			}
		}
	}
}
