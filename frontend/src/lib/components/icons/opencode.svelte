<script lang="ts">
  let { size = 24, ...restProps } = $props();
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  viewBox="0 0 24 24"
  width={size}
  height={size}
  fill="currentColor"
  {...restProps}>
  <path d="M22 24H2V0h20zM17 4.8H7v14.4h10z" />
</svg>
