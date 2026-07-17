/** @type {import('next').NextConfig} */
const nextConfig = {
  // Bundle the release tarballs into the download function on Vercel.
  outputFileTracingIncludes: {
    "/api/download": ["./releases/**"],
  },
  async redirects() {
    return [{ source: "/", destination: "/admin", permanent: false }];
  },
};

export default nextConfig;
