// Package password holds the password policy: at least 12 characters, at most
// 72 bytes (the most bcrypt uses), and at least one letter and one digit. The
// first-admin set-up, the user screens and the emailed password reset all
// check new passwords with Validate (the store's seed directly, the web layer
// through auth.ValidatePassword and auth.HashPassword).
package password
