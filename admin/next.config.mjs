/** @type {import('next').NextConfig} */
const nextConfig = {
  async redirects() {
    return [{ source: "/", destination: "/admin", permanent: false }];
  },
};

export default nextConfig;
