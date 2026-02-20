Theorem complex_goal_flow : forall (A B C : Prop),
  A -> B -> C -> (A /\ B) /\ C.
Proof.
  intros A B C HA HB HC.
  assert (HAB : A /\ B).
  { split.
    - exact HA.
    - exact HB. }
  split.
  - exact HAB.
  - exact HC.
Qed.
