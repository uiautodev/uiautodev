import type { Rect, UINode } from "$lib/api/schemas";

export interface XPathCandidate {
	xpath: string;
	label: string;
	unique: boolean;
	matchCount: number;
	isFirst: boolean;
	rank: number;
}

// 候选属性组合，label 直接用组合字符串。
// 含 "class" 的用元素选择器 //{tagName}，否则用全局 //*。
const LOCATOR_SPECS = [
	"resource-id",
	"identifier",
	"rawIdentifier",
	"id",
	"content-desc",
	"label",
	"name",
	"value",
	"text",
	"class",
	"class+resource-id",
	"class+identifier",
	"class+rawIdentifier",
	"class+id",
	"class+content-desc",
	"class+label",
	"class+name",
	"class+value",
	"class+text",
];

interface NodeEntry {
	id: string;
	tagName: string;
	rect: Rect;
	props: Record<string, any>;
}

interface TreeIndex {
	nodes: NodeEntry[];
	idToIndex: Map<string, number>;
	tagIndex: Map<string, Set<number>>;
	attrIndex: Map<string, Map<string, Set<number>>>;
}

// 按 root 引用缓存索引；root 变化（重新 dump）时自动重建，旧条目随 GC 释放。
const treeIndexCache = new WeakMap<UINode, TreeIndex>();

function buildTreeIndex(root: UINode): TreeIndex {
	const nodes: NodeEntry[] = [];
	const idToIndex = new Map<string, number>();
	const tagIndex = new Map<string, Set<number>>();
	const attrIndex = new Map<string, Map<string, Set<number>>>();

	const walk = (node: UINode) => {
		const idx = nodes.length;
		nodes.push({
			id: node.id,
			tagName: node.tagName,
			rect: node.rect,
			props: node.props ?? {},
		});
		idToIndex.set(node.id, idx);

		if (node.tagName) {
			let s = tagIndex.get(node.tagName);
			if (!s) {
				s = new Set();
				tagIndex.set(node.tagName, s);
			}
			s.add(idx);
		}

		const props = node.props ?? {};
		for (const [key, raw] of Object.entries(props)) {
			if (raw == null) continue;
			let m = attrIndex.get(key);
			if (!m) {
				m = new Map();
				attrIndex.set(key, m);
			}
			const value = String(raw).trim();
			if (!value) continue;
			let s = m.get(value);
			if (!s) {
				s = new Set();
				m.set(value, s);
			}
			s.add(idx);
		}

		for (const child of node.children ?? []) {
			walk(child);
		}
	};

	walk(root);
	return { nodes, idToIndex, tagIndex, attrIndex };
}

function getTreeIndex(root: UINode): TreeIndex {
	let index = treeIndexCache.get(root);
	if (!index) {
		index = buildTreeIndex(root);
		treeIndexCache.set(root, index);
	}
	return index;
}

/**
 * 将字符串值安全地写成 XPath 字面量。
 * 优先单引号，其次双引号，同时包含两种引号时退回 concat() 拼接。
 */
export function xpathStringLiteral(value: string): string {
	if (!value.includes("'")) return `'${value}'`;
	if (!value.includes('"')) return `"${value}"`;
	const parts = value.split("'");
	return `concat(${parts.map((part) => `'${part}'`).join(", \"'\", ")})`;
}

function intersectSet(a: Set<number> | undefined, b: Set<number> | undefined): Set<number> {
	const result = new Set<number>();
	if (!a || !b) return result;
	if (a.size > b.size) {
		const tmp = a;
		a = b;
		b = tmp;
	}
	for (const v of a) {
		if (b.has(v)) result.add(v);
	}
	return result;
}

function rankOf(s: Set<number> | undefined, targetIndex: number): number {
	if (!s || s.size === 0 || targetIndex < 0) return 0;
	let rank = 0;
	for (const v of s) {
		if (v <= targetIndex) rank++;
	}
	return rank;
}

/**
 * 为选中的元素生成一组基于可读属性的 XPath 候选。
 * 每个候选对应一个属性组合，优先输出能唯一定位的，全部基于预建索引，无树遍历。
 */
export function generateXPathCandidates(
	root: UINode | null,
	target: UINode | null,
): XPathCandidate[] {
	if (!root || !target) return [];

	const index = getTreeIndex(root);
	const props = target.props ?? {};
	const tag = target.tagName;
	const targetIndex = index.idToIndex.get(target.id) ?? -1;
	const candidates: XPathCandidate[] = [];
	const seen = new Set<string>();

	for (const spec of LOCATOR_SPECS) {
		const isClassOnly = spec === "class";
		const attrName = isClassOnly ? "" : spec.includes("+") ? spec.slice(spec.indexOf("+") + 1) : spec;
		const usesTag = spec === "class" || spec.includes("+");

		let value = "";
		let matchSet: Set<number> | undefined;
		if (isClassOnly) {
			if (!tag) continue;
			matchSet = index.tagIndex.get(tag);
		} else {
			const raw = props[attrName];
			if (raw == null) continue;
			value = String(raw).trim();
			if (!value) continue;
			if (usesTag) {
				if (!tag) continue;
				matchSet = intersectSet(index.tagIndex.get(tag), index.attrIndex.get(attrName)?.get(value));
			} else {
				matchSet = index.attrIndex.get(attrName)?.get(value);
			}
		}

		if (!matchSet?.has(targetIndex)) continue;
		const count = matchSet.size;
		const rank = rankOf(matchSet, targetIndex);
		const isFirst = rank === 1;

		const xpath = isClassOnly
			? `//${tag}`
			: usesTag
				? `//${tag}[@${attrName}=${xpathStringLiteral(value)}]`
				: `//*[@${attrName}=${xpathStringLiteral(value)}]`;
		if (seen.has(xpath)) continue;
		seen.add(xpath);

		candidates.push({ xpath, label: spec, unique: count === 1, matchCount: count, isFirst, rank });
	}

	return candidates;
}
