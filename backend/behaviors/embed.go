// Package behaviors carries the behaviour scripts into the binary.
//
// Layout: one directory per behaviour, and the directory name is the code a
// catalog node names in service_nodes.behavior_code.
//
//	behaviors/
//	  verification/
//	    config.star     constants and everything the script exchanges with the
//	                    core: amounts, roles, event names, messages
//	    behavior.star   MANIFEST and the hooks
//
// config.star is executed first and its globals are visible to the rest of the
// behaviour, so a rule and the number it uses are separate edits.
//
// The scripts are data, not Go code — that is the point of them — but they must
// travel with the deployment, and a container that has to mount a directory to
// start is a worse default than one that already contains its behaviours. A
// directory (BEHAVIORS_DIR) with the same layout is still read on top of these
// at startup, so a rule can be corrected without rebuilding the image.
package behaviors

import "embed"

//go:embed */*.star
var FS embed.FS
