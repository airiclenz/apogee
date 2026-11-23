// Package gitconfigkeys is the one catalogue of the git config keys whose value reaches a program:
// which names git executes as a command, which further names a `git config` line can set to
// reach one, and which values of those name no program after all.
//
// Two packages judge by it and neither restates a pattern:
//
//   - internal/gitexec compiles [Source] into the probe that refuses a repository whose own
//     config names a program, and [MergeDriverSource] into the one member it neutralises per call
//     instead.
//   - internal/security compiles [WriteViewSource] into the shell write view, which names
//     .git/config for a `git config` line that sets such a key, and clears the values
//     [ValueIsBenign] reports.
//
// Which key belongs on which side is one rule (ADR 0049, amendments 1103 and 1114): a key
// apogee's own git can reach goes in [Source]; a key the git hosts already neutralise goes in
// [WriteViewSuffix] only.
//
// The package is a leaf: it imports the standard library and nothing else. internal/gitexec
// imports internal/security, so neither of them could hold the catalogue for the other.
package gitconfigkeys
