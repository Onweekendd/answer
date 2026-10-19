/**
 * An image this site stores: under /uploads/ on this host. The port is not compared: in
 * development the page comes from vite and the images from the Go server on another port.
 */
export function isOwnUpload(src: string): boolean {
  try {
    const url = new URL(src, window.location.href);
    return url.hostname === window.location.hostname && url.pathname.startsWith("/uploads/");
  } catch {
    return false;
  }
}
