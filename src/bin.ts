#!/usr/bin/env node
import { run } from './cli.js';

// A separate executable avoids import-meta/argv path comparisons breaking npm's bin symlinks.
process.exitCode = await run(process.argv.slice(2));
