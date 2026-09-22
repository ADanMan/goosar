export function GET(request: Request) {
  return Response.redirect(new URL('/goosar-icon.svg', request.url), 308);
}
