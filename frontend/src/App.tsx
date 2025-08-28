import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import { Toaster } from 'sonner'
import HomePage from './pages/HomePage'
import OnlineMigrationPage from './pages/OnlineMigrationPage'
import OfflineMigrationPage from './pages/OfflineMigrationPage'
import StatusPage from './pages/StatusPage'
import Layout from './components/Layout'

function App() {
  return (
    <Router>
      <div className="min-h-screen bg-gray-50">
        <Layout>
          <Routes>
            <Route path="/" element={<HomePage />} />
            <Route path="/online-migration" element={<OnlineMigrationPage />} />
            <Route path="/offline-migration" element={<OfflineMigrationPage />} />
            <Route path="/status/:taskId" element={<StatusPage />} />
          </Routes>
        </Layout>
        <Toaster position="top-right" />
      </div>
    </Router>
  )
}

export default App