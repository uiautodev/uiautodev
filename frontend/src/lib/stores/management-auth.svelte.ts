/**
 * managementAuth keeps the management session token for the current SPA
 * session. The settings page stores it after a password check so other pages
 * (for example the MCP page) can read protected values without asking again.
 */
export const managementAuth = $state({
  token: "",
});
