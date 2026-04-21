package main

import (
	"flag"
	"fmt"
	"os"

	indexer "github.com/jamespritchard/code-indexer"
)

func main() {
	var (
		workers int
		format  string
	)

	flag.IntVar(&workers, "workers", 4, "Number of concurrent workers")
	flag.StringVar(&format, "format", "json", "Output format: json or compact")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: code-indexer [flags] <directory>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	idx := indexer.NewTSIndexer()
	report, err := idx.IndexDir(args[0], workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	compact := format == "compact"
	output, err := indexer.SerializeReport(report, compact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.Write(output)
	os.Stdout.WriteString("\n")
}
