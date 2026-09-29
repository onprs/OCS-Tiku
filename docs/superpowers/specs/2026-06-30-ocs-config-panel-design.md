# OCS Question Bank Configuration Panel Design

## Context
The user requested adding a display and quick copy area for the OCS question bank configuration on the frontend dashboard of the `ocs-tiku` project. The backend (`dashboard.py`) already exposes this configuration via the `/api/config` endpoint, but the React frontend (`Dashboard.tsx`) currently lacks a UI to display it.

## Architecture & Components

### 1. Location
The new component will be added to `frontend/src/pages/Dashboard.tsx`.
It will be placed exactly between the top metric cards (Today's Requests, Total Questions, Success Rate) and the bottom "Trend Analysis" chart.

### 2. UI Structure
We will create a collapsible section (implemented via standard React state and `framer-motion` for smooth height animations) contained within a `Card` component.

#### Default State (Collapsed):
- A standard card header.
- **Title**: "🚀 OCS 题库配置"
- **Action**: An "一键复制" (Quick Copy) button aligned to the right. Clicking it will copy the configuration directly, without requiring the user to expand the panel. It will trigger a toast notification upon success.
- **Toggle**: The header area will be clickable (cursor-pointer) to toggle the expanded state.

#### Expanded State:
- A `CardContent` section that reveals itself with a vertical slide-down animation.
- A `<pre>` or code block area displaying the formatted JSON configuration.
- A short instruction text below the code block:
  > 💡 复制后，打开 OCS 脚本悬浮窗 → 通用 → 全局设置 → 题库配置，解析器选择「默认」，粘贴并保存即可。

### 3. Data Flow
- We will use `useSWR` to fetch data from the `/api/config` endpoint.
- Since the backend `build_ocs_config` returns a Python list (JSON array), we will `JSON.stringify(data, null, 2)` to format it cleanly for the frontend.
- The `copyText` functionality will utilize the browser's native `navigator.clipboard.writeText` API, and standard UI toasts (`useToast` hook) will be shown for success/failure feedback.

## Error Handling
- If the `/api/config` endpoint fails to load, the panel will display a concise error state ("加载配置失败").
- If the clipboard API fails (e.g., due to lack of HTTPS/localhost context, though it's typically fine on localhost), a fallback error toast will be displayed.

## Testing Strategy
- Ensure clicking the header toggles the visibility correctly.
- Ensure clicking the "Copy" button works independently of the collapse state and doesn't trigger the collapse toggle (using `e.stopPropagation()`).
- Verify the JSON output matches the expected OCS format.