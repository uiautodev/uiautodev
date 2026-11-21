<script lang="ts">
	import { onDestroy } from "svelte";
	import type { Snippet } from "svelte";
	import { Button, type ButtonProps } from "$lib/components/ui/button/index.js";
	import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
	import CheckIcon from "@lucide/svelte/icons/check";

	let {
		loading = false,
		success = $bindable(false),
		successDelay = 1000,
		loadingLabel = "加载中",
		successLabel = "已保存",
		successClass = "text-emerald-600 dark:text-emerald-400",
		children,
		class: className,
		...rest
	}: ButtonProps & {
		loading?: boolean;
		success?: boolean;
		successDelay?: number;
		loadingLabel?: string;
		successLabel?: string;
		successClass?: string;
		children?: Snippet;
	} = $props();

	let successTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		clearTimeout(successTimer);
		if (success) {
			successTimer = setTimeout(() => {
				success = false;
			}, successDelay);
		}
	});

	onDestroy(() => clearTimeout(successTimer));
</script>

<Button class={className} {...rest}>
	{#if loading}
		<LoaderCircleIcon class="animate-spin" />
		{loadingLabel}
	{:else if success}
		<CheckIcon class={successClass} />
		{successLabel}
	{:else}
		{@render children?.()}
	{/if}
</Button>
