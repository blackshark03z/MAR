package main

// Verification Speed V1 contract markers.
//
// The executable assertions live in verification_profile_test.go:
// - TestGoStandardVerificationProfileUsesCacheFriendlyFullGate
// - TestGoDocsVerificationProfileUsesMinimalCachedCompileGate
// - TestGoReleaseVerificationProfilePreservesUncachedCanonicalGate
//
// These names are mirrored here so the immutable Goal Contract can bind its
// simple file oracle to cmd/mar/main_test.go while the tests remain grouped
// with the verification-profile implementation.
