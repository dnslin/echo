function App() {
  return (
    <main className="container" aria-labelledby="app-title">
      <section className="brand" aria-label="echo bootstrap status">
        <span className="brand-badge">Wails 3</span>
        <span className="brand-badge">React</span>
        <span className="brand-badge">TypeScript</span>
      </section>

      <h1 id="app-title" className="title">
        echo 桌面端已就绪
      </h1>
      <p className="subtitle">
        工程骨架已准备好，后续 MVP 功能将在独立任务中接入。
      </p>
    </main>
  )
}

export default App
