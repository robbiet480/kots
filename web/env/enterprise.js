module.exports = {
  ENVIRONMENT: "production",
  SECURE_ADMIN_CONSOLE: true,
  API_ENDPOINT: "//",
  SHIP_CLUSTER_BUILD_VERSION: (function () {
    return String(Date.now());
  }()),
};

