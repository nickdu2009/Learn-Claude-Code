# Role: Expert Frontend Developer
# Task: Build a React + Vite Todo Application
# Guidelines: Write clean, modular, and performant code. Ensure all functional requirements are met. Do not use external UI libraries (e.g., AntD, MUI) unless specified. Use standard CSS or CSS Modules.

## 1. Tech Stack
- Build Tool: Vite
- Framework: React 18+ (Strict functional components, React Hooks)
- Language: JavaScript (or TypeScript if preferred)
- State Management: React useState, useEffect
- Persistence: browser localStorage

## 2. Data Interfaces
```typescript
interface Todo {
  id: string; // Use crypto.randomUUID() or Date.now().toString()
  text: string;
  completed: boolean;
}

type FilterType = 'All' | 'Active' | 'Completed';

```

## 3. File Structure

Create the following file structure:

```text
src/
  ├── App.jsx            # Main container & state manager
  ├── App.css            # Global styles
  ├── components/
  │   ├── TodoInput.jsx  # Input form
  │   ├── TodoList.jsx   # Renders TodoItems based on filter
  │   ├── TodoItem.jsx   # Individual task UI
  │   └── TodoFooter.jsx # Filter controls & stats
  ├── main.jsx           # Entry point

```

## 4. State Management (in App.jsx)

* `todos`: Array of `Todo` objects.
* `filter`: Current filter state (default: 'All').
* **Persistence constraint:** Implement a `useEffect` that synchronizes the `todos` state to `localStorage` (key: 'todo-list-data') whenever `todos` changes. Initialize `todos` by reading from `localStorage` on first mount.

## 5. Component Specifications & Props

### 5.1 App.jsx

* **Responsibilities:** Holds global state, defines mutating functions, passes them down as props.
* **Functions to implement:**
* `addTodo(text: string)`
* `toggleTodo(id: string)`
* `deleteTodo(id: string)`
* `clearCompleted()`
* `setFilter(filter: FilterType)`



### 5.2 TodoInput.jsx

* **Props:** `{ addTodo }`
* **Behavior:**
* Input field for new tasks.
* Submit on "Enter" key or button click.
* Prevent submission of empty or whitespace-only strings.
* Clear input after successful submission.



### 5.3 TodoList.jsx

* **Props:** `{ todos, filter, toggleTodo, deleteTodo }`
* **Behavior:**
* Compute `filteredTodos` based on `todos` and `filter` state.
* Render a `TodoItem` for each item in `filteredTodos`.



### 5.4 TodoItem.jsx

* **Props:** `{ todo, toggleTodo, deleteTodo }`
* **Behavior:**
* Checkbox to toggle `completed` state.
* Text display (apply `text-decoration: line-through` and gray color if `completed === true`).
* Delete button.



### 5.5 TodoFooter.jsx

* **Props:** `{ todos, filter, setFilter, clearCompleted }`
* **Behavior:**
* Display "X items left" (count of `completed === false`).
* Render 3 filter buttons: "All", "Active", "Completed". Highlight the currently active filter.
* Render "Clear completed" button (only visible/enabled if there is at least one completed todo).



## 6. Execution Steps

1. Initialize project using `npm create vite@latest . --template react`.
2. Clear default boilerplate in `App.jsx` and `App.css`.
3. Create the components directory and files as specified.
4. Implement logic and styling.
5. Provide the complete code for each file.

