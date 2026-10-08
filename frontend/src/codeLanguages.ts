// Display list for the code-block language picker (Editor.tsx /
// CodeBlockView.tsx). `value` is what actually gets stored on the
// codeBlock node's `language` attribute and must match a name lowlight
// has registered -- see the `common` bundle passed into
// `createLowlight()` in Editor.tsx. `null` means "no highlighting",
// stored as a real null rather than the string 'plaintext' so an
// unhighlighted block round-trips the same way a freshly created one
// (no language chosen yet) does.
export interface CodeLanguage {
  value: string | null
  label: string
}

export const CODE_LANGUAGES: CodeLanguage[] = [
  { value: null, label: 'Plain text' },
  { value: 'javascript', label: 'JavaScript' },
  { value: 'typescript', label: 'TypeScript' },
  { value: 'python', label: 'Python' },
  { value: 'python-repl', label: 'Python (REPL)' },
  { value: 'go', label: 'Go' },
  { value: 'rust', label: 'Rust' },
  { value: 'java', label: 'Java' },
  { value: 'kotlin', label: 'Kotlin' },
  { value: 'csharp', label: 'C#' },
  { value: 'c', label: 'C' },
  { value: 'cpp', label: 'C++' },
  { value: 'objectivec', label: 'Objective-C' },
  { value: 'swift', label: 'Swift' },
  { value: 'php', label: 'PHP' },
  { value: 'php-template', label: 'PHP Template' },
  { value: 'ruby', label: 'Ruby' },
  { value: 'perl', label: 'Perl' },
  { value: 'lua', label: 'Lua' },
  { value: 'r', label: 'R' },
  { value: 'sql', label: 'SQL' },
  { value: 'shell', label: 'Shell' },
  { value: 'bash', label: 'Bash' },
  { value: 'graphql', label: 'GraphQL' },
  { value: 'json', label: 'JSON' },
  { value: 'yaml', label: 'YAML' },
  { value: 'xml', label: 'XML / HTML' },
  { value: 'css', label: 'CSS' },
  { value: 'scss', label: 'SCSS' },
  { value: 'less', label: 'Less' },
  { value: 'markdown', label: 'Markdown' },
  { value: 'ini', label: 'INI' },
  { value: 'diff', label: 'Diff' },
  { value: 'makefile', label: 'Makefile' },
  { value: 'vbnet', label: 'VB.NET' },
  { value: 'wasm', label: 'WebAssembly' },
  { value: 'arduino', label: 'Arduino' },
]
