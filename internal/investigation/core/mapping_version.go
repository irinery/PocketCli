package core

// MappingVersion identifies how source records are projected into the public
// dataset. Snapshots keep the version that produced them; a new run publishes
// the current one.
//
// 1.18.0: Linear state type "duplicate" is classified as duplicate, and
// complete Linear issue histories project workflow-stage intervals.
const MappingVersion = "1.18.0"
