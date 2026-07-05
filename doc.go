// Package gitspork exposes the top-level operations of the gitspork tool as a
// Go library. See https://github.com/rockholla/gitspork for the full CLI
// documentation.
//
// The three entry points are Integrate, IntegrateLocal, and CheckDrift. Each
// returns a structural result alongside an error, so consumers can inspect
// what was integrated or which files drifted without parsing log output.
//
// Example — check-drift bot:
//
//	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
//	    DownstreamRepoPath: "/path/to/downstream",
//	})
//	if err != nil && err != gitspork.ErrDriftDetected {
//	    log.Fatal(err)
//	}
//	for _, f := range report.Files {
//	    log.Printf("drifted: %s (attributed to %s)", f.Path, f.AttributedURL)
//	}
//
// Example — fleet integrator:
//
//	for _, downstream := range downstreamDirs {
//	    result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
//	        Upstreams: []gitspork.UpstreamSpec{{
//	            URL:     "git@github.com:org/platform.git",
//	            Version: "v1.2.0",
//	        }},
//	        DownstreamRepoPath: downstream,
//	    })
//	    if err != nil {
//	        log.Printf("failed on %s: %v", downstream, err)
//	        continue
//	    }
//	    log.Printf("%s: integrated %d upstream(s)", downstream, len(result.Upstreams))
//	}
package gitspork
