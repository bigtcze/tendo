import { useRef, useState } from 'react';
import { readRoute, type Route } from './routes';

export function useAppRoute() {
  const [route, setRoute] = useState<Route>(() => readRoute());
  const transitioned = useRef(false);
  const [allItemsVisit, setAllItemsVisit] = useState(0);
  return { route, setRoute, transitioned, allItemsVisit, setAllItemsVisit };
}
