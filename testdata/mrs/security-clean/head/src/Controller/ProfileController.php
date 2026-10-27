<?php

namespace App\Controller;

use App\Entity\Profile;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/profiles')]
#[IsGranted('ROLE_USER')]
class ProfileController extends AbstractController
{
    #[Route('/{id}', name: 'profile_show')]
    #[IsGranted('PROFILE_VIEW', 'profile')]
    public function show(Profile $profile): Response
    {
        return $this->render('profile/show.html.twig', ['profile' => $profile]);
    }
}
