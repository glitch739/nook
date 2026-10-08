// Starts a download through a temporary link (the server answers with
// Content-Disposition: attachment, so the page itself stays put).
export function downloadUrl(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = ''
  document.body.append(a)
  a.click()
  a.remove()
}
