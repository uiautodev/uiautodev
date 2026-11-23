<script lang="ts">
	import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
	import { buttonVariants } from "$lib/components/ui/button/index.js";
	import type { ButtonVariant } from "$lib/components/ui/button/index.js";
	import { cn } from "$lib/utils.js";
	import { onMount } from "svelte";
	import type { Snippet } from "svelte";

	interface Props {
		ref?: any;
		class?: string;
		open: boolean;
		seconds?: number;
		variant?: ButtonVariant;
		children: Snippet;
	}

	let {
		ref = $bindable(null),
		class: className,
		open,
		seconds = 3,
		variant = 'outline',
		children,
		onclick,
		...restProps
	}: Props & Omit<AlertDialogPrimitive.CancelProps, 'children'> = $props();

	let countdown = $state(0);
	let timer: ReturnType<typeof setInterval> | null = null;
	let startTime = 0;
	let paused = $state(document.hidden); // 页面隐藏时暂停倒计时

	function handleClick(e: any) {
		if (countdown > 0) {
			e.preventDefault();
			e.stopPropagation();
			return;
		}
		if (onclick) {
			onclick(e);
		}
	}

	function startCountdown() {
		// 清除之前的定时器
		if (timer) {
			clearInterval(timer);
			timer = null;
		}

		startTime = Date.now();
		countdown = seconds;
		paused = false;

		timer = setInterval(() => {
			const elapsed = (Date.now() - startTime) / 1000;
			const remaining = Math.max(0, seconds - elapsed);
			countdown = Math.ceil(remaining);

			if (remaining <= 0) {
				if (timer) clearInterval(timer);
				timer = null;
				countdown = 0;
			}
		}, 100);
	}

	function stopCountdown() {
		if (timer) {
			clearInterval(timer);
			timer = null;
		}
		paused = true;
	}

	function handleVisibilityChange() {
		if (document.hidden) {
			// 页面隐藏时停止倒计时
			if (countdown > 0) {
				stopCountdown();
			}
		} else {
			// 页面可见时恢复倒计时
			if (paused && countdown > 0) {
				startCountdown();
			}
		}
	}

	// 监听 open 变化
	$effect(() => {
		if (open) {
			startCountdown();
		} else {
			if (timer) {
				clearInterval(timer);
				timer = null;
			}
			countdown = 0;
		}
	});

	onMount(() => {
		document.addEventListener("visibilitychange", handleVisibilityChange);
		return () => {
			document.removeEventListener("visibilitychange", handleVisibilityChange);
			if (timer) clearInterval(timer);
		};
	});
</script>

<AlertDialogPrimitive.Cancel
	bind:ref
	data-slot="alert-dialog-cancel-countdown"
	class={cn(
		buttonVariants({ variant }),
		className,
		countdown > 0 && "opacity-50 cursor-not-allowed"
	)}
	onclick={handleClick}
	{...restProps}
>
	{#if countdown > 0}
		{@render children()}
		({countdown}s)
	{:else}
		{@render children()}
	{/if}
</AlertDialogPrimitive.Cancel>
