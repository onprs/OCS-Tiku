import { lazy } from "react";
import { Routes, Route } from "react-router-dom";
import { MainLayout } from "./components/layout/MainLayout";
import { Toaster } from "@/components/ui/toaster";

const Dashboard = lazy(() => import("./pages/Dashboard"));
const Records = lazy(() => import("./pages/Records"));
const Settings = lazy(() => import("./pages/Settings"));

function App() {
  return (
    <>
      <Routes>
        <Route path="/" element={<MainLayout />}>
          <Route index element={<Dashboard />} />
          <Route path="records" element={<Records />} />
          <Route path="settings" element={<Settings />} />
        </Route>
      </Routes>
      <Toaster />
    </>
  );
}

export default App;
