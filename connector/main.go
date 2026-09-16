package main

import (
	"flag"
	"fmt"
	"os"

	helpers "github.com/fatjonblp/coding_challange_integrations/connector/helpers/go"
)

type Args struct {
	run_id         string
	erp_base_url   string
	twin_base_url  string
	erp_export_dir string
	twin_drop_dir  string
	state_dir      string
	report_dir     string
	config         string
	no_chaos       bool
}

func ParseArgs(args []string) Args {
	fs := flag.NewFlagSet("blp-connector", flag.ExitOnError)

	runid := fs.String("run-id", "", "run id")
	erpbaseurl := fs.String("erp-base-url", "", "ERP Base URL")
	twinbaseurl := fs.String("twin-base-url", "", "Twin Base URL")
	erpexportdir := fs.String("erp-export-dir", "", "Erp Export Directory")
	twindropdir := fs.String("twin-drop-dir", "", "Twin Drop Directory")
	statedir := fs.String("state-dir", "", "State Directory")
	reportdir := fs.String("report-dir", "", "Report Directory")
	config := fs.String("config", "", "Additional Config")
	nochaos := fs.Bool("no-chaos", false, "No-Chaos")

	fs.Parse(args)

	return Args{
		run_id:         *runid,
		erp_base_url:   *erpbaseurl,
		twin_base_url:  *twinbaseurl,
		erp_export_dir: *erpexportdir,
		twin_drop_dir:  *twindropdir,
		state_dir:      *statedir,
		report_dir:     *reportdir,
		config:         *config,
		no_chaos:       *nochaos,
	}
}

const defaultVersion = "0.0.0-dev"

func connectorVersion() string {
	if v := os.Getenv("CONNECTOR_VERSION"); v != "" {
		return v
	}
	return defaultVersion
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(connectorVersion())
		os.Exit(0)
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: connector --version | incorrect call")
		os.Exit(3)
		return
	}
	args := ParseArgs(os.Args[2:])

	erpClient, err := NewClient(args.erp_base_url, "/erp/v1/auth/token",
		os.Getenv("ERP_CLIENT_ID"), os.Getenv("ERP_CLIENT_SECRET"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "erp: authentication failed:", err)
		os.Exit(3)
		return
	}

	twinClient, err := NewClient(args.twin_base_url, "/v1/auth/token",
		os.Getenv("TWIN_CLIENT_ID"), os.Getenv("TWIN_CLIENT_SECRET"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "twin: authentication failed:", err)
		os.Exit(3)
		return
	}

	// Verify health on erp side. No token needed
	if err := helpers.Healthz(args.erp_base_url); err != nil {
		fmt.Fprintln(os.Stderr, "erp: health check failed:", err)
		os.Exit(3)
		return
	}

	// Phase 1: master data.
	masterData, err := SyncMasterData(erpClient, twinClient, args.run_id, args.state_dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "phase 1 (master data): failed:", err)
		os.Exit(3)
		return
	}
	fmt.Fprintf(os.Stderr, "phase 1 (master data): full_load=%v read=%v accepted=%v rejected=%v\n",
		masterData.FullLoad, masterData.Read, masterData.Accepted, masterData.Rejected)

	// TODO: fase 2 (legacy)
	//  fase 3 (FX)
	//  fase 4 (posting)
}
