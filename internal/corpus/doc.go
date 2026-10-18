// SPDX-License-Identifier: MIT

// Package corpus generates testdata/corpus: packet traces and MBCAP
// captures of representative scenarios (a plant, a gateway, a function-code
// tour, for tests, screenshots and demos), of hard but valid input (lossy,
// odd link types, midstream, TLS, clocks, IPv6) and of broken input (empty,
// garbage, cut, corrupt, misnamed). Every file is synthetic and generated
// deterministically by this package's test (make golden); manifest.json
// says what each one is and how the application must take it, and the
// engine's corpus test, the fuzz test and make smoke-corpus hold it to that.
package corpus
