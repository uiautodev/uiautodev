/**
 * XPath validation utilities
 */

/**
 * Validates basic XPath syntax
 * @param xpath - XPath expression to validate
 * @returns true if syntax appears valid, false otherwise
 */
export function isValidXPathSyntax(xpath: string): boolean {
	if (!xpath.trim()) return false;

	// Check for balanced brackets
	let openBrackets = 0;
	let openSquare = 0;

	for (let i = 0; i < xpath.length; i++) {
		const char = xpath[i];
		if (char === '(') openBrackets++;
		else if (char === ')') {
			if (openBrackets === 0) return false; // Closing without opening
			openBrackets--;
		}
		else if (char === '[') openSquare++;
		else if (char === ']') {
			if (openSquare === 0) return false;
			openSquare--;
		}
	}

	if (openBrackets !== 0 || openSquare !== 0) return false;

	// Check for basic XPath patterns (starts with //, /, or .)
	const hasValidStart = /^(\/\/?|\/|\.)/.test(xpath.trim());
	if (!hasValidStart) return false;

	return true;
}
