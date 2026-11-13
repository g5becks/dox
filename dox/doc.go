// Package dox embeds dox in Go programs: load a project, sync its sources, and
// read or search the synced documentation without shelling out to the CLI.
//
// The API mirrors the commands an agent already knows: [Project.Sync],
// [Project.Collections], [Project.Files], [Project.Outline], [Project.Cat] and
// [Project.Search]. All methods return an [*Error] carrying a stable [Code]
// and a hint, and wrap sentinels such as [ErrCollectionNotFound] for
// errors.Is.
//
// A [Project] keeps no mutable state: it only remembers the loaded
// configuration, and every read goes to disk, so a Project may be used from
// several goroutines. Syncs of one output directory are serialized by an
// advisory lock shared with the CLI.
//
// The package is new in v0.5.0 and may change before v1.0.0; incompatible
// changes are listed under "Changed" in the CHANGELOG.
package dox
