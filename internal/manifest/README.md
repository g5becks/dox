# Manifest generation

The previous manifest is a versioned parse cache. `parser.CurrentVersion` must
be bumped whenever extraction semantics change. A matching parser revision and
collection identity permit reuse when the file's provider SHA, size and mtime
match; sources without a provider SHA use size and mtime. Metadata checks also
invalidate locally edited GitHub files even when the lock has not changed.
Missing files disappear, and a stale parser revision forces regeneration.

Walking stays inside `os.Root`; file workers use `DirEntry.Info` and parse changed
files in a shared errgroup bounded by GOMAXPROCS. Results merge after workers
finish, with collections and file paths sorted before encoding. Source state
files and ownership/temp files are excluded from document entries.

Publication uses compact JSON and a root-confined atomic replacement. An
unchanged snapshot is not rewritten. Context cancellation is checked before
walking, during discovery/work, after reads and immediately before atomic rename;
a cancelled run leaves the previous manifest intact.

`generated` is the latest input file/sync timestamp, not the generation wall
clock. `last_sync` comes from the lock, or the collection's latest file mtime
when no lock timestamp exists. An empty timestamp-less snapshot uses zero time.
All these timestamps use UTC. These rules make separate full generations of the
same inputs byte-identical. Existing manifests load normally and regenerate once
to add `parser_version` and optional per-file `sha` cache metadata.
